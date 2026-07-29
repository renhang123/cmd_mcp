package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type serverEntry struct {
	Name      string `json:"name"`
	Host      string `json:"host"`
	User      string `json:"user"`
	SSHKey    string `json:"ssh_key"`
	ProfileID string `json:"profile_id"`
	Artifact  string `json:"artifact"`
}

type serverList struct {
	Servers []serverEntry `json:"servers"`
}

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type callResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

type toolResult struct {
	Status        string                 `json:"status"`
	ErrorCategory string                 `json:"error_category"`
	Message       string                 `json:"message"`
	Data          map[string]interface{} `json:"data"`
	CommandResult *commandResultPayload  `json:"command_result"`
}

type commandResultPayload struct {
	Status   string
	ExitCode int
	Stdout   string
	Stderr   string
	Message  string
}

func (r *commandResultPayload) UnmarshalJSON(data []byte) error {
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	r.Status = stringField(payload, "status", "Status")
	r.Stdout = stringField(payload, "stdout", "Stdout")
	r.Stderr = stringField(payload, "stderr", "Stderr")
	r.Message = stringField(payload, "message", "Message")
	r.ExitCode = intField(payload, "exit_code", "ExitCode")
	return nil
}

func stringField(payload map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok {
			return value
		}
	}
	return ""
}

func intField(payload map[string]interface{}, keys ...string) int {
	for _, key := range keys {
		switch value := payload[key].(type) {
		case float64:
			return int(value)
		case int:
			return value
		}
	}
	return 0
}

type mcpSession struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	nextID int
}

func main() {
	configPath := flag.String("config", "deploy-servers.json", "path to the deploy servers config file")
	serverName := flag.String("server", "", "server name from the config file (required)")
	artifactOverride := flag.String("artifact", "", "override the artifact path configured for the server")
	flag.Parse()

	if *serverName == "" {
		fmt.Fprintln(os.Stderr, "-server is required")
		flag.Usage()
		os.Exit(2)
	}

	server, err := loadServer(*configPath, *serverName)
	if err != nil {
		fatal("%v", err)
	}

	artifactPath := server.Artifact
	if *artifactOverride != "" {
		artifactPath = *artifactOverride
	}
	if artifactPath == "" {
		fatal("no artifact configured for server %q; pass -artifact", server.Name)
	}
	artifactPath = expandHome(artifactPath)

	info, err := os.Stat(artifactPath)
	if err != nil {
		fatal("stat artifact: %v", err)
	}
	digest, err := fileSHA256(artifactPath)
	if err != nil {
		fatal("hash artifact: %v", err)
	}
	artifactName := filepath.Base(artifactPath)
	fmt.Printf("artifact: %s (%d bytes, sha256 %s)\n", artifactPath, info.Size(), digest)

	session, err := dial(server)
	if err != nil {
		fatal("connect: %v", err)
	}
	defer session.close()

	if err := session.initialize(); err != nil {
		fatal("initialize: %v", err)
	}
	fmt.Printf("connected: %s@%s\n", server.User, server.Host)

	if err := runDeploy(session, server, artifactPath, artifactName, info.Size(), digest); err != nil {
		fatal("%v", err)
	}
}

func runDeploy(session *mcpSession, server serverEntry, artifactPath, artifactName string, size int64, digest string) error {
	begin, err := session.callTool("artifact_upload_begin", map[string]interface{}{
		"profile_id":    server.ProfileID,
		"artifact_name": artifactName,
		"size_bytes":    size,
		"sha256":        digest,
	})
	if err != nil {
		return fmt.Errorf("upload begin: %v", err)
	}
	uploadID, _ := begin.Data["upload_id"].(string)
	if uploadID == "" {
		return fmt.Errorf("upload begin: server did not return upload_id")
	}
	maxChunk := int64(512 * 1024)
	if v, ok := begin.Data["max_chunk_bytes"].(float64); ok && int64(v) < maxChunk {
		maxChunk = int64(v)
	}
	fmt.Printf("upload session: %s (chunk %d bytes)\n", uploadID, maxChunk)

	uploaded := false
	defer func() {
		if !uploaded {
			_, _ = session.callTool("artifact_upload_abort", map[string]interface{}{"upload_id": uploadID})
		}
	}()

	if err := uploadChunks(session, artifactPath, uploadID, size, maxChunk); err != nil {
		return err
	}

	commit, err := session.callTool("artifact_upload_commit", map[string]interface{}{"upload_id": uploadID})
	if err != nil {
		return fmt.Errorf("upload commit: %v", err)
	}
	uploaded = true
	artifactID, _ := commit.Data["artifact_id"].(string)
	if artifactID == "" {
		return fmt.Errorf("upload commit: server did not return artifact_id")
	}
	fmt.Printf("committed: %s\n", artifactID)

	deploy, err := session.callTool("deploy_artifact", map[string]interface{}{
		"profile_id":  server.ProfileID,
		"artifact_id": artifactID,
	})
	if err != nil {
		return fmt.Errorf("deploy: %v", err)
	}
	if deploy.CommandResult != nil {
		if deploy.CommandResult.Stdout != "" {
			fmt.Print(deploy.CommandResult.Stdout)
		}
		if deploy.CommandResult.Stderr != "" {
			fmt.Fprint(os.Stderr, deploy.CommandResult.Stderr)
		}
		if deploy.CommandResult.Status != "success" {
			return fmt.Errorf("deploy command status: %s (exit %d)", deploy.CommandResult.Status, deploy.CommandResult.ExitCode)
		}
	}
	fmt.Println("deploy finished")
	return nil
}

func uploadChunks(session *mcpSession, artifactPath, uploadID string, size, maxChunk int64) error {
	file, err := os.Open(artifactPath)
	if err != nil {
		return fmt.Errorf("open artifact: %v", err)
	}
	defer file.Close()

	buf := make([]byte, maxChunk)
	var offset int64
	for {
		n, readErr := io.ReadFull(file, buf)
		if n > 0 {
			chunk := buf[:n]
			_, err := session.callTool("artifact_upload_chunk", map[string]interface{}{
				"upload_id":   uploadID,
				"offset":      offset,
				"data_base64": base64.StdEncoding.EncodeToString(chunk),
			})
			if err != nil {
				return fmt.Errorf("upload chunk at offset %d: %v", offset, err)
			}
			offset += int64(n)
			fmt.Printf("\ruploaded %d/%d bytes (%.1f%%)", offset, size, float64(offset)*100/float64(size))
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("read artifact: %v", readErr)
		}
	}
	fmt.Println()
	return nil
}

func loadServer(path, name string) (serverEntry, error) {
	data, err := ioutil.ReadFile(expandHome(path))
	if err != nil {
		return serverEntry{}, fmt.Errorf("read config: %v", err)
	}
	var list serverList
	if err := json.Unmarshal(data, &list); err != nil {
		return serverEntry{}, fmt.Errorf("parse config: %v", err)
	}
	for _, server := range list.Servers {
		if server.Name == name {
			if server.Host == "" || server.User == "" || server.ProfileID == "" {
				return serverEntry{}, fmt.Errorf("server %q is missing host, user or profile_id", name)
			}
			return server, nil
		}
	}
	return serverEntry{}, fmt.Errorf("server %q not found in %s", name, path)
}

func dial(server serverEntry) (*mcpSession, error) {
	args := []string{"-T", "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes"}
	if server.SSHKey != "" {
		args = append(args, "-i", expandHome(server.SSHKey))
	}
	args = append(args, server.User+"@"+server.Host)

	cmd := exec.Command("ssh", args...)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &mcpSession{cmd: cmd, stdin: stdin, reader: bufio.NewReader(stdout), nextID: 1}, nil
}

func (s *mcpSession) close() {
	_ = s.stdin.Close()
	_ = s.cmd.Wait()
}

func (s *mcpSession) initialize() error {
	if _, err := s.rpc("initialize", map[string]interface{}{}); err != nil {
		return err
	}
	// notifications/initialized has no id and no response.
	notification := rpcRequest{JSONRPC: "2.0", Method: "notifications/initialized"}
	data, err := json.Marshal(notification)
	if err != nil {
		return err
	}
	_, err = s.stdin.Write(append(data, '\n'))
	return err
}

func (s *mcpSession) rpc(method string, params interface{}) (json.RawMessage, error) {
	id := s.nextID
	s.nextID++
	request := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	if _, err := s.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("write request: %v", err)
	}
	line, err := s.reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read response: %v", err)
	}
	var response rpcResponse
	if err := json.Unmarshal(line, &response); err != nil {
		return nil, fmt.Errorf("parse response: %v", err)
	}
	if response.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", response.Error.Code, response.Error.Message)
	}
	return response.Result, nil
}

func (s *mcpSession) callTool(name string, arguments map[string]interface{}) (*toolResult, error) {
	result, err := s.rpc("tools/call", map[string]interface{}{"name": name, "arguments": arguments})
	if err != nil {
		return nil, err
	}
	var call callResult
	if err := json.Unmarshal(result, &call); err != nil {
		return nil, fmt.Errorf("parse tool result: %v", err)
	}
	if len(call.Content) == 0 {
		return nil, fmt.Errorf("tool %s returned no content", name)
	}
	var tool toolResult
	if err := json.Unmarshal([]byte(call.Content[0].Text), &tool); err != nil {
		return nil, fmt.Errorf("parse tool %s payload: %v", name, err)
	}
	if call.IsError || tool.Status != "success" {
		if tool.ErrorCategory != "" {
			return &tool, fmt.Errorf("%s: %s (%s)", name, tool.Message, tool.ErrorCategory)
		}
		return &tool, fmt.Errorf("%s: %s", name, tool.Message)
	}
	return &tool, nil
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func expandHome(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}

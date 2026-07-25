package artifact

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"server-shell-mcp/internal/app"
)

type StoreConfig struct {
	Enabled        bool   `json:"enabled"`
	RootDirectory  string `json:"root_directory"`
	MaxUploadBytes int64  `json:"max_upload_bytes"`
	MaxChunkBytes  int64  `json:"max_chunk_bytes"`
}

type DeployProfile struct {
	ID                  string `json:"id"`
	Description         string `json:"description"`
	ArtifactNamePattern string `json:"artifact_name_pattern"`
	DeployCommandID     string `json:"deploy_command_id"`
}

type CommandService interface {
	Execute(ctx context.Context, req app.CommandRequest) app.CommandResult
}

type Service struct {
	store    StoreConfig
	profiles map[string]DeployProfile
	commands CommandService
	mu       sync.Mutex
	sessions map[string]*uploadSession
}

type uploadSession struct {
	ID           string
	ProfileID    string
	ArtifactName string
	SizeBytes    int64
	SHA256       string
	TempPath     string
	Received     int64
	CreatedAt    time.Time
}

type Manifest struct {
	ArtifactID   string    `json:"artifact_id"`
	ArtifactName string    `json:"artifact_name"`
	ProfileID    string    `json:"profile_id"`
	SizeBytes    int64     `json:"size_bytes"`
	SHA256       string    `json:"sha256"`
	CommittedAt  time.Time `json:"committed_at"`
}

type Result struct {
	RequestID     string                 `json:"request_id"`
	ToolName      string                 `json:"tool_name"`
	Status        app.Status             `json:"status"`
	ErrorCategory app.ErrorCategory      `json:"error_category,omitempty"`
	Message       string                 `json:"message"`
	Data          map[string]interface{} `json:"data,omitempty"`
	CommandResult *app.CommandResult     `json:"command_result,omitempty"`
}

func NewService(store StoreConfig, profiles []DeployProfile, commands CommandService) (*Service, error) {
	profileMap := make(map[string]DeployProfile, len(profiles))
	for _, profile := range profiles {
		profileMap[profile.ID] = profile
	}
	if err := os.MkdirAll(filepath.Join(store.RootDirectory, "uploads"), 0700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(store.RootDirectory, "committed"), 0700); err != nil {
		return nil, err
	}
	return &Service{store: store, profiles: profileMap, commands: commands, sessions: map[string]*uploadSession{}}, nil
}

func (s *Service) Tools() []ToolSpec {
	if !s.store.Enabled {
		return nil
	}
	return []ToolSpec{
		{Name: "artifact_upload_begin", Description: "Begin a controlled artifact upload session.", RiskLevel: "medium"},
		{Name: "artifact_upload_chunk", Description: "Upload one base64 artifact chunk to an existing session.", RiskLevel: "medium"},
		{Name: "artifact_upload_commit", Description: "Verify and commit a completed artifact upload.", RiskLevel: "high"},
		{Name: "artifact_upload_abort", Description: "Abort a pending artifact upload session.", RiskLevel: "medium"},
		{Name: "deploy_artifact", Description: "Deploy a committed artifact through a fixed deploy profile.", RiskLevel: "high"},
	}
}

type ToolSpec struct {
	Name        string
	Description string
	RiskLevel   string
}

func (s *Service) Call(ctx context.Context, requestID string, toolName string, args map[string]interface{}, source app.SourceSummary) Result {
	switch toolName {
	case "artifact_upload_begin":
		return s.begin(requestID, toolName, args)
	case "artifact_upload_chunk":
		return s.chunk(requestID, toolName, args)
	case "artifact_upload_commit":
		return s.commit(requestID, toolName, args)
	case "artifact_upload_abort":
		return s.abort(requestID, toolName, args)
	case "deploy_artifact":
		return s.deploy(ctx, requestID, toolName, args, source)
	default:
		return rejected(requestID, toolName, app.ErrorNotFound, "Artifact tool is not registered.")
	}
}

func (s *Service) HasTool(name string) bool {
	for _, tool := range s.Tools() {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func (s *Service) begin(requestID string, toolName string, args map[string]interface{}) Result {
	profileID, ok := stringArg(args, "profile_id")
	if !ok {
		return rejected(requestID, toolName, app.ErrorValidation, "profile_id is required.")
	}
	artifactName, ok := stringArg(args, "artifact_name")
	if !ok {
		return rejected(requestID, toolName, app.ErrorValidation, "artifact_name is required.")
	}
	sizeBytes, ok := int64Arg(args, "size_bytes")
	if !ok || sizeBytes <= 0 {
		return rejected(requestID, toolName, app.ErrorValidation, "size_bytes must be a positive integer.")
	}
	expectedSHA, ok := stringArg(args, "sha256")
	if !ok || !validSHA256(expectedSHA) {
		return rejected(requestID, toolName, app.ErrorValidation, "sha256 must be lowercase hex.")
	}
	profile, ok := s.profiles[profileID]
	if !ok {
		return rejected(requestID, toolName, app.ErrorValidation, "profile_id is not allowed.")
	}
	if sizeBytes > s.store.MaxUploadBytes {
		return rejected(requestID, toolName, app.ErrorPolicyDenied, "artifact exceeds max upload size.")
	}
	if !safeArtifactName(artifactName) {
		return rejected(requestID, toolName, app.ErrorValidation, "artifact_name format is invalid.")
	}
	matched, err := regexp.MatchString(profile.ArtifactNamePattern, artifactName)
	if err != nil || !matched {
		return rejected(requestID, toolName, app.ErrorPolicyDenied, "artifact_name is not allowed by profile.")
	}
	uploadID, err := randomID("upload")
	if err != nil {
		return rejected(requestID, toolName, app.ErrorInternal, "upload id could not be generated.")
	}
	tempPath := filepath.Join(s.store.RootDirectory, "uploads", uploadID+".part")
	file, err := os.OpenFile(tempPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return rejected(requestID, toolName, app.ErrorInternal, "upload session could not be created.")
	}
	_ = file.Close()
	s.mu.Lock()
	s.sessions[uploadID] = &uploadSession{ID: uploadID, ProfileID: profileID, ArtifactName: artifactName, SizeBytes: sizeBytes, SHA256: expectedSHA, TempPath: tempPath, CreatedAt: time.Now()}
	s.mu.Unlock()
	return success(requestID, toolName, "Upload session created.", map[string]interface{}{"upload_id": uploadID, "max_chunk_bytes": s.store.MaxChunkBytes})
}

func (s *Service) chunk(requestID string, toolName string, args map[string]interface{}) Result {
	uploadID, ok := stringArg(args, "upload_id")
	if !ok || !validToken(uploadID) {
		return rejected(requestID, toolName, app.ErrorValidation, "upload_id is required.")
	}
	offset, ok := int64Arg(args, "offset")
	if !ok || offset < 0 {
		return rejected(requestID, toolName, app.ErrorValidation, "offset must be a non-negative integer.")
	}
	encoded, ok := stringArg(args, "data_base64")
	if !ok {
		return rejected(requestID, toolName, app.ErrorValidation, "data_base64 is required.")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return rejected(requestID, toolName, app.ErrorValidation, "data_base64 is invalid.")
	}
	if int64(len(data)) == 0 || int64(len(data)) > s.store.MaxChunkBytes {
		return rejected(requestID, toolName, app.ErrorPolicyDenied, "chunk size is not allowed.")
	}
	s.mu.Lock()
	session, ok := s.sessions[uploadID]
	if !ok {
		s.mu.Unlock()
		return rejected(requestID, toolName, app.ErrorNotFound, "upload session was not found.")
	}
	if offset != session.Received {
		s.mu.Unlock()
		return rejected(requestID, toolName, app.ErrorValidation, "chunk offset is not the next expected offset.")
	}
	if session.Received+int64(len(data)) > session.SizeBytes {
		s.mu.Unlock()
		return rejected(requestID, toolName, app.ErrorPolicyDenied, "chunk exceeds declared upload size.")
	}
	file, err := os.OpenFile(session.TempPath, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		s.mu.Unlock()
		return rejected(requestID, toolName, app.ErrorInternal, "upload file could not be opened.")
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		s.mu.Unlock()
		return rejected(requestID, toolName, app.ErrorInternal, "chunk could not be written.")
	}
	session.Received += int64(len(data))
	received := session.Received
	expected := session.SizeBytes
	s.mu.Unlock()
	return success(requestID, toolName, "Chunk accepted.", map[string]interface{}{"upload_id": uploadID, "received_bytes": received, "size_bytes": expected})
}

func (s *Service) commit(requestID string, toolName string, args map[string]interface{}) Result {
	uploadID, ok := stringArg(args, "upload_id")
	if !ok || !validToken(uploadID) {
		return rejected(requestID, toolName, app.ErrorValidation, "upload_id is required.")
	}
	s.mu.Lock()
	session, ok := s.sessions[uploadID]
	if !ok {
		s.mu.Unlock()
		return rejected(requestID, toolName, app.ErrorNotFound, "upload session was not found.")
	}
	if session.Received != session.SizeBytes {
		s.mu.Unlock()
		return rejected(requestID, toolName, app.ErrorValidation, "upload is incomplete.")
	}
	delete(s.sessions, uploadID)
	s.mu.Unlock()

	digest, err := fileSHA256(session.TempPath)
	if err != nil {
		return rejected(requestID, toolName, app.ErrorInternal, "artifact checksum could not be calculated.")
	}
	if digest != session.SHA256 {
		_ = os.Remove(session.TempPath)
		return rejected(requestID, toolName, app.ErrorValidation, "artifact sha256 does not match.")
	}
	artifactID := strings.TrimSuffix(session.ArtifactName, artifactExtension(session.ArtifactName)) + "-" + digest[:12]
	if !validArtifactID(artifactID) {
		_ = os.Remove(session.TempPath)
		return rejected(requestID, toolName, app.ErrorValidation, "artifact id format is invalid.")
	}
	committedPath := filepath.Join(s.store.RootDirectory, "committed", artifactID+artifactExtension(session.ArtifactName))
	manifestPath := filepath.Join(s.store.RootDirectory, "committed", artifactID+".json")
	if _, err := os.Stat(committedPath); err == nil {
		_ = os.Remove(session.TempPath)
		return rejected(requestID, toolName, app.ErrorPolicyDenied, "artifact is already committed.")
	}
	manifest := Manifest{ArtifactID: artifactID, ArtifactName: session.ArtifactName, ProfileID: session.ProfileID, SizeBytes: session.SizeBytes, SHA256: digest, CommittedAt: time.Now().UTC()}
	manifestData, err := json.Marshal(manifest)
	if err != nil {
		_ = os.Remove(session.TempPath)
		return rejected(requestID, toolName, app.ErrorInternal, "artifact manifest could not be created.")
	}
	if err := os.Rename(session.TempPath, committedPath); err != nil {
		return rejected(requestID, toolName, app.ErrorInternal, "artifact could not be committed.")
	}
	if err := ioutil.WriteFile(manifestPath, append(manifestData, '\n'), 0600); err != nil {
		_ = os.Remove(committedPath)
		return rejected(requestID, toolName, app.ErrorInternal, "artifact manifest could not be written.")
	}
	return success(requestID, toolName, "Artifact committed.", map[string]interface{}{"artifact_id": artifactID, "artifact_path": committedPath, "profile_id": session.ProfileID, "size_bytes": session.SizeBytes, "sha256": digest})
}

func (s *Service) abort(requestID string, toolName string, args map[string]interface{}) Result {
	uploadID, ok := stringArg(args, "upload_id")
	if !ok || !validToken(uploadID) {
		return rejected(requestID, toolName, app.ErrorValidation, "upload_id is required.")
	}
	s.mu.Lock()
	session, ok := s.sessions[uploadID]
	if ok {
		delete(s.sessions, uploadID)
	}
	s.mu.Unlock()
	if ok {
		_ = os.Remove(session.TempPath)
	}
	return success(requestID, toolName, "Upload session aborted.", map[string]interface{}{"upload_id": uploadID})
}

func (s *Service) deploy(ctx context.Context, requestID string, toolName string, args map[string]interface{}, source app.SourceSummary) Result {
	profileID, ok := stringArg(args, "profile_id")
	if !ok {
		return rejected(requestID, toolName, app.ErrorValidation, "profile_id is required.")
	}
	artifactID, ok := stringArg(args, "artifact_id")
	if !ok || !validArtifactID(artifactID) {
		return rejected(requestID, toolName, app.ErrorValidation, "artifact_id is required.")
	}
	profile, ok := s.profiles[profileID]
	if !ok {
		return rejected(requestID, toolName, app.ErrorValidation, "profile_id is not allowed.")
	}
	manifestPath := filepath.Join(s.store.RootDirectory, "committed", artifactID+".json")
	manifest, err := readManifest(manifestPath)
	if err != nil {
		return rejected(requestID, toolName, app.ErrorValidation, "artifact manifest is missing or invalid.")
	}
	if manifest.ProfileID != profileID {
		return rejected(requestID, toolName, app.ErrorPolicyDenied, "artifact is not committed for this profile.")
	}
	ext := artifactExtension(manifest.ArtifactName)
	if ext == "" {
		return rejected(requestID, toolName, app.ErrorValidation, "artifact name format is invalid.")
	}
	artifactPath := filepath.Join(s.store.RootDirectory, "committed", artifactID+ext)
	if !insideRoot(artifactPath, filepath.Join(s.store.RootDirectory, "committed")) {
		return rejected(requestID, toolName, app.ErrorPolicyDenied, "artifact path is outside committed root.")
	}
	if _, err := os.Stat(artifactPath); err != nil {
		return rejected(requestID, toolName, app.ErrorNotFound, "artifact was not found.")
	}
	result := s.commands.Execute(ctx, app.CommandRequest{RequestID: requestID, CommandID: profile.DeployCommandID, Arguments: map[string]interface{}{"artifact_path": artifactPath}, Source: app.SourceSummary{ClientIDHash: source.ClientIDHash, UserIDHash: source.UserIDHash, RemoteHash: source.RemoteHash, Transport: source.Transport, MCPTool: toolName}})
	status := result.Status
	category := result.ErrorCategory
	message := "Deploy command completed."
	if status != app.StatusSuccess {
		message = "Deploy command failed."
	}
	return Result{RequestID: requestID, ToolName: toolName, Status: status, ErrorCategory: category, Message: message, Data: map[string]interface{}{"profile_id": profileID, "artifact_id": artifactID, "artifact_path": artifactPath, "deploy_command_id": profile.DeployCommandID}, CommandResult: &result}
}

func readManifest(path string) (Manifest, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func success(requestID string, toolName string, message string, data map[string]interface{}) Result {
	return Result{RequestID: requestID, ToolName: toolName, Status: app.StatusSuccess, Message: message, Data: data}
}

func rejected(requestID string, toolName string, category app.ErrorCategory, message string) Result {
	return Result{RequestID: requestID, ToolName: toolName, Status: app.StatusRejected, ErrorCategory: category, Message: message}
}

func stringArg(args map[string]interface{}, name string) (string, bool) {
	value, ok := args[name].(string)
	return value, ok && value != ""
}

func int64Arg(args map[string]interface{}, name string) (int64, bool) {
	switch value := args[name].(type) {
	case int:
		return int64(value), true
	case int64:
		return value, true
	case float64:
		if value != float64(int64(value)) {
			return 0, false
		}
		return int64(value), true
	case json.Number:
		parsed, err := value.Int64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func randomID(prefix string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return prefix + "-" + hex.EncodeToString(buf), nil
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

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')) {
			return false
		}
	}
	return true
}

func validToken(value string) bool {
	matched, _ := regexp.MatchString(`^[a-z]+-[a-f0-9]{32}$`, value)
	return matched
}

func validArtifactID(value string) bool {
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`, value)
	return matched && !strings.Contains(value, "..") && !strings.ContainsAny(value, `/\\`)
}

func safeArtifactName(value string) bool {
	return validArtifactID(value) && artifactExtension(value) != ""
}

// artifactExtension returns the supported archive extension of name,
// or "" when the name does not end in a supported extension.
func artifactExtension(name string) string {
	for _, ext := range []string{".tar.gz", ".zip"} {
		if strings.HasSuffix(name, ext) {
			return ext
		}
	}
	return ""
}

func insideRoot(path string, root string) bool {
	cleanPath := filepath.Clean(path)
	cleanRoot := filepath.Clean(root)
	return cleanPath == cleanRoot || strings.HasPrefix(cleanPath, cleanRoot+string(filepath.Separator))
}

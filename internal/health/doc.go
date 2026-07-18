package health

type Status string

const (
	StatusOK       Status = "ok"
	StatusNotReady Status = "not_ready"
)

type Result struct {
	Status  Status
	Message string
}

type Checker struct {
	configValid  bool
	dependencies []Dependency
}

type Dependency interface {
	Ready() bool
}

func NewChecker(configValid bool, dependencies ...Dependency) *Checker {
	return &Checker{configValid: configValid, dependencies: dependencies}
}

func (c *Checker) Live() Result {
	return Result{Status: StatusOK, Message: "process is running"}
}

func (c *Checker) Ready() Result {
	if !c.configValid {
		return Result{Status: StatusNotReady, Message: "configuration is invalid"}
	}
	for _, dependency := range c.dependencies {
		if !dependency.Ready() {
			return Result{Status: StatusNotReady, Message: "dependency is not ready"}
		}
	}
	return Result{Status: StatusOK, Message: "service is ready"}
}

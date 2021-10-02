package common

const (
	DeployRoute   = "/api/v1/deploy"
	RollbackRoute = "/api/v1/deploy/rollback"
)

type DeployReq struct {
	Name   string `json:"name"`
	Bundle string `json:"bundle"`
}

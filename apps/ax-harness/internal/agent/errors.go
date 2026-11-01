package agent

import (
	"errors"

	ax "github.com/ax-llm/ax/packages/go"
)

func actorStepsExhausted(err error) bool {
	var axErr ax.AxError
	return errors.As(err, &axErr) && axErr.Category == "runtime" && axErr.Message == "agent actor loop exceeded max steps"
}

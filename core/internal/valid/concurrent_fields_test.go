package valid

import (
	"encoding/json"
	"strconv"
	"sync"
	"testing"

	"github.com/dosco/graphjin/core/v3/internal/qcode"
)

func TestFieldValidatorsCanBeSharedAcrossRequests(t *testing.T) {
	for _, name := range []string{"equalsField", "notEqualsField", "greaterThanField", "greaterThanOrEqualsField", "lessThanField", "lessThanOrEqualsField"} {
		t.Run(name, func(t *testing.T) {
			fn, err := Validators[name].NewFn([]string{"reference"})
			if err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			start := make(chan struct{})
			for request := 0; request < 8; request++ {
				wg.Add(1)
				go func(request int) {
					defer wg.Done()
					<-start
					for j := 0; j < 200; j++ {
						n := request*1000 + j
						target := n
						switch name {
						case "notEqualsField", "greaterThanField":
							target = n + 1
						case "lessThanField":
							target = n - 1
						}
						vars := qcode.Vars{"reference": json.RawMessage(strconv.Itoa(n)), "target": json.RawMessage(strconv.Itoa(target))}
						if !fn(vars, qcode.Constraint{VarName: "target"}) {
							t.Errorf("request%d iteration%d rejected", request, j)
							return
						}
					}
				}(request)
			}
			close(start)
			wg.Wait()
		})
	}
}

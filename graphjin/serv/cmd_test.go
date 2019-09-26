package serv_test

import (
	"testing"
)

func TestErrorLineExtract(t *testing.T) {
	tests := []struct {
		source   string
		position int
		ele      ErrorLineExtract
		errMsg   string
	}{
		{
			source:   "single line",
			position: 3,
			ele: ErrorLineExtract{
				LineNum:   1,
				ColumnNum: 3,
				Text:      "single line",
			},
			errMsg: "",
		},
		{
			source:   "bad position",
			position: 32,
			ele:      ErrorLineExtract{},
			errMsg:   "position (32) is greater than source length (12)",
		},
		{
			source: `multi
line
text`,
			position: 8,
			ele: ErrorLineExtract{
				LineNum:   2,
				ColumnNum: 2,
				Text:      "line",
			},
			errMsg: "",
		},
		{
			source: `last
line
error`,
			position: 13,
			ele: ErrorLineExtract{
				LineNum:   3,
				ColumnNum: 3,
				Text:      "error",
			},
			errMsg: "",
		},
		{
			source: `first
character
first
line
error`,
			position: 1,
			ele: ErrorLineExtract{
				LineNum:   1,
				ColumnNum: 1,
				Text:      "first",
			},
			errMsg: "",
		},
		{
			source: `last
character
last
line
error`,
			position: 30,
			ele: ErrorLineExtract{
				LineNum:   5,
				ColumnNum: 5,
				Text:      "error",
			},
			errMsg: "",
		},
	}

	for i, tt := range tests {
		ele, err := ExtractErrorLine(tt.source, tt.position)
		if err != nil {
			if tt.errMsg == "" {
				t.Errorf("%d. Expected success but received err %v", i, err)
			} else if err.Error() != tt.errMsg {
				t.Errorf("%d. Expected err %v, but received %v", i, tt.errMsg, err)
			}
			continue
		}

		if tt.errMsg != "" {
			t.Errorf("%d. Expected err %v, but it succeeded", i, tt.errMsg)
			continue
		}

		if ele != tt.ele {
			t.Errorf("%d. Expected %v, but received %v", i, tt.ele, ele)
		}
	}
}

package psql

import (
	"io"
)

func (md *Metadata) RenderVar(w io.Writer, vv string) {
	f, s := -1, 0

	for i := range vv {
		v := vv[i]
		switch {
		case (i > 0 && vv[i-1] != '\\' && v == '$') || v == '$':
			if (i - s) > 0 {
				_, _ = io.WriteString(w, vv[s:i])
			}
			f = i

		case (v < 'a' && v > 'z') &&
			(v < 'A' && v > 'Z') &&
			(v < '0' && v > '9') &&
			v != '_' &&
			f != -1 &&
			(i-f) > 1:
			md.renderValueExp(w, Param{Name: vv[f+1 : i]})
			s = i
			f = -1
		}
	}

	if f != -1 && (len(vv)-f) > 1 {
		md.renderValueExp(w, Param{Name: vv[f+1:]})
	} else {
		_, _ = io.WriteString(w, vv[s:])
	}
}

func (md *Metadata) renderValueExp(w io.Writer, p Param) {
	_, _ = io.WriteString(w, `$`)
	if v, ok := md.pindex[p.Name]; ok {
		int32String(w, int32(v))

	} else {
		md.params = append(md.params, p)
		n := len(md.params)

		if md.pindex == nil {
			md.pindex = make(map[string]int)
		}
		md.pindex[p.Name] = n
		int32String(w, int32(n))
	}
}

func (md Metadata) Skipped() uint32 {
	return md.skipped
}

func (md Metadata) Params() []Param {
	return md.params
}

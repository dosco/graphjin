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
			md.renderParam(w, Param{Name: vv[f+1 : i]})
			s = i
			f = -1
		}
	}

	if f != -1 && (len(vv)-f) > 1 {
		md.renderParam(w, Param{Name: vv[f+1:]})
	} else {
		_, _ = io.WriteString(w, vv[s:])
	}
}

func (md *Metadata) renderParam(w io.Writer, p Param) {
	var id int
	var ok bool

	if !md.Poll {
		_, _ = io.WriteString(w, `$`)
	}

	if id, ok = md.pindex[p.Name]; !ok {
		md.params = append(md.params, p)
		id = len(md.params)

		if md.pindex == nil {
			md.pindex = make(map[string]int)
		}
		md.pindex[p.Name] = id
	}

	if md.Poll {
		_, _ = io.WriteString(w, `"_sg_sub".`)
		quoted(w, p.Name)
	} else {
		int32String(w, int32(id))
	}
}

func (md Metadata) HasRemotes() bool {
	return md.remoteCount != 0
}

func (md Metadata) Remotes() int {
	return md.remoteCount
}

func (md Metadata) Params() []Param {
	return md.params
}

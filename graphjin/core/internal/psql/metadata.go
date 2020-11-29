package psql

import (
	"bytes"
	"strings"
)

// nolint: errcheck
func (md *Metadata) RenderVar(w *bytes.Buffer, vv string) {
	f, s := -1, 0

	for i := range vv {
		v := vv[i]
		switch {
		case (i > 0 && vv[i-1] != '\\' && v == '$') || v == '$':
			if (i - s) > 0 {
				w.WriteString(vv[s:i])
			}
			f = i

		case f != -1 &&
			!(v >= 'a' && v <= 'z') &&
			!(v >= 'A' && v <= 'Z') &&
			!(v >= '0' && v <= '9') &&
			v != '_' &&
			v != ':':
			name, _type := parseVar(vv[f+1 : i])
			md.renderParam(w, Param{Name: name, Type: _type})
			s = i
			f = -1

		case f != -1 && i == (len(vv)-1):
			name, _type := parseVar(vv[f+1 : i+1])
			md.renderParam(w, Param{Name: name, Type: _type})
			s = i + 1
			f = -1
		}
	}

	if f == -1 && s != len(vv) {
		w.WriteString(vv[s:])
	}
}

// nolint: errcheck
func (md *Metadata) renderParam(w *bytes.Buffer, p Param) {
	var id int
	var ok bool

	if !md.poll {
		w.WriteString(`$`)
	}

	if id, ok = md.pindex[p.Name]; !ok {
		md.params = append(md.params, p)
		id = len(md.params)

		if md.pindex == nil {
			md.pindex = make(map[string]int)
		}
		md.pindex[p.Name] = id
	}

	if md.poll {
		_, _ = w.WriteString(`"_sg_sub".`)
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

func parseVar(v string) (string, string) {
	dt := "text"
	if n := strings.IndexByte(v, ':'); n != -1 {
		if t := v[n+1:]; t != "" {
			return v[:n], t
		}
		return v[:n], dt
	}
	return v, dt
}

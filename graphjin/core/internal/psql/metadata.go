package psql

import (
	"bytes"
	"strings"
)

func (md *Metadata) RenderVar(w *bytes.Buffer, vv string) {
	(&compilerContext{md: md, w: w}).renderVar(vv)
}

// nolint: errcheck
func (c *compilerContext) renderVar(vv string) {
	f, s := -1, 0

	for i := range vv {
		v := vv[i]
		switch {
		case (i > 0 && vv[i-1] != '\\' && v == '$') || v == '$':
			if (i - s) > 0 {
				c.w.WriteString(vv[s:i])
			}
			f = i

		case f != -1 &&
			!(v >= 'a' && v <= 'z') &&
			!(v >= 'A' && v <= 'Z') &&
			!(v >= '0' && v <= '9') &&
			v != '_' &&
			v != ':':
			name, _type := parseVar(vv[f+1 : i])
			c.renderParam(Param{Name: name, Type: _type})
			s = i
			f = -1

		case f != -1 && i == (len(vv)-1):
			name, _type := parseVar(vv[f+1 : i+1])
			c.renderParam(Param{Name: name, Type: _type})
			s = i + 1
			f = -1
		}
	}

	if f == -1 && s != len(vv) {
		c.w.WriteString(vv[s:])
	}
}

// nolint: errcheck
func (c *compilerContext) renderParam(p Param) {
	var id int
	var ok bool

	md := c.md
	if id, ok = md.pindex[p.Name]; !ok {
		md.params = append(c.md.params, p)
		id = len(c.md.params)

		if md.pindex == nil {
			md.pindex = make(map[string]int)
		}
		md.pindex[p.Name] = id
	}

	if md.poll {
		_, _ = c.w.WriteString(`_sg_sub.`)
		quoted(c.w, p.Name)

	} else {
		switch c.md.ct {
		case "mysql":
			c.w.WriteString(`?`)
		default:
			c.w.WriteString(`$`)
			int32String(c.w, int32(id))
		}
	}
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

package qcode

type Config struct {
	Blocklist []string
	KeepArgs  bool
}

type QueryConfig struct {
	Limit            int
	Filter           []string
	Columns          []string
	DisableFunctions bool
}

type InsertConfig struct {
	Filter  []string
	Columns []string
	Set     map[string]string
}

type UpdateConfig struct {
	Filter  []string
	Columns []string
	Set     map[string]string
}

type DeleteConfig struct {
	Filter  []string
	Columns []string
}

type TRConfig struct {
	Query  QueryConfig
	Insert InsertConfig
	Update UpdateConfig
	Delete DeleteConfig
}

type trval struct {
	query struct {
		limit   string
		fil     *Exp
		cols    map[string]struct{}
		disable struct {
			funcs bool
		}
	}

	insert struct {
		fil  *Exp
		cols map[string]struct{}
		set  map[string]string
	}

	update struct {
		fil  *Exp
		cols map[string]struct{}
		set  map[string]string
	}

	delete struct {
		fil  *Exp
		cols map[string]struct{}
	}
}

func (trv *trval) allowedColumns(qt QType) map[string]struct{} {
	switch qt {
	case QTQuery:
		return trv.query.cols
	case QTInsert:
		return trv.insert.cols
	case QTUpdate:
		return trv.update.cols
	case QTDelete:
		return trv.insert.cols
	case QTUpsert:
		return trv.insert.cols
	}

	return nil
}

func (trv *trval) filter(qt QType) *Exp {
	switch qt {
	case QTQuery:
		return trv.query.fil
	case QTInsert:
		return trv.insert.fil
	case QTUpdate:
		return trv.update.fil
	case QTDelete:
		return trv.delete.fil
	case QTUpsert:
		return trv.insert.fil
	}

	return nil
}

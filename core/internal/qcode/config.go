package qcode

type Config struct {
	Vars            map[string]string
	TConfig         map[string]TConfig
	DefaultBlock    bool
	DefaultLimit    int
	DisableAgg      bool
	DisableFuncs    bool
	EnableCamelcase bool
	DBSchema        string
	Validators      map[string]Validator

	defTrv trval
}

type TConfig struct {
	OrderBy map[string][][2]string
}

type TRConfig struct {
	Query  QueryConfig
	Insert InsertConfig
	Update UpdateConfig
	Upsert UpsertConfig
	Delete DeleteConfig
}

type QueryConfig struct {
	Limit            int
	Filters          []string
	Columns          []string
	DisableFunctions bool
	Block            bool
}

type InsertConfig struct {
	Columns []string
	Presets map[string]string
	Block   bool
}

type UpdateConfig struct {
	Filters []string
	Columns []string
	Presets map[string]string
	Block   bool
}

type UpsertConfig struct {
	Filters []string
	Columns []string
	Presets map[string]string
	Block   bool
}

type DeleteConfig struct {
	Filters []string
	Columns []string
	Block   bool
}

type trval struct {
	role string

	query struct {
		limit   int32
		fil     *Exp
		filNU   bool
		cols    map[string]struct{}
		disable struct{ funcs bool }
		block   bool
	}

	insert struct {
		cols    map[string]struct{}
		presets map[string]string
		block   bool
	}

	update struct {
		fil     *Exp
		filNU   bool
		cols    map[string]struct{}
		presets map[string]string
		block   bool
	}

	upsert struct {
		fil     *Exp
		filNU   bool
		cols    map[string]struct{}
		presets map[string]string
		block   bool
	}

	delete struct {
		fil   *Exp
		filNU bool
		cols  map[string]struct{}
		block bool
	}
}

func (co *Compiler) AddRole(role, schema, table string, trc TRConfig) error {
	var err error

	ti, err := co.Find(schema, table)
	if err != nil {
		return err
	}

	trv := trval{role: role}

	// query config
	trv.query.fil, trv.query.filNU, err = compileFilter(co.s, ti, trc.Query.Filters, false)
	if err != nil {
		return err
	}

	if trc.Query.Limit > 0 {
		trv.query.limit = int32(trc.Query.Limit)
	}
	trv.query.cols = makeSet(trc.Query.Columns)
	trv.query.disable.funcs = trc.Query.DisableFunctions
	trv.query.block = trc.Query.Block

	// insert config
	trv.insert.cols = makeSet(trc.Insert.Columns)
	trv.insert.presets = trc.Insert.Presets
	trv.insert.block = trc.Insert.Block

	// update config
	trv.update.fil, trv.update.filNU, err = compileFilter(co.s, ti, trc.Update.Filters, false)
	if err != nil {
		return err
	}
	trv.update.cols = makeSet(trc.Update.Columns)
	trv.update.presets = trc.Update.Presets
	trv.update.block = trc.Update.Block

	// upsert config
	trv.upsert.fil, trv.upsert.filNU, err = compileFilter(co.s, ti, trc.Upsert.Filters, false)
	if err != nil {
		return err
	}
	trv.upsert.cols = makeSet(trc.Upsert.Columns)
	trv.upsert.presets = trc.Upsert.Presets
	trv.upsert.block = trc.Upsert.Block

	// delete config
	trv.delete.fil, trv.delete.filNU, err = compileFilter(co.s, ti, trc.Delete.Filters, false)
	if err != nil {
		return err
	}
	trv.delete.cols = makeSet(trc.Delete.Columns)
	trv.delete.block = trc.Delete.Block

	if schema == "" {
		schema = co.s.DBSchema()
	}
	co.tr[(role + ":" + schema + ":" + table)] = trv

	return nil
}

func (co *Compiler) getRole(role, schema, table, field string) trval {
	var k string

	if co.s.IsAlias(field) {
		k = (role + ":" + schema + ":" + field)
	} else {
		k = (role + ":" + schema + ":" + table)
	}

	// For anon roles when a trval is not found return the default trval
	tr, ok := co.tr[k]
	tr.role = role

	if !ok && role == "anon" {
		return co.c.defTrv
	}
	return tr
}

func (co *Compiler) getTConfig(schema, name string) TConfig {
	return co.c.TConfig[(schema + name)]
}

func (trv *trval) filter(qt QType) (*Exp, bool) {
	switch qt {
	case QTQuery:
		return trv.query.fil, trv.query.filNU
	case QTInsert:
		return nil, false
	case QTUpdate:
		return trv.update.fil, trv.update.filNU
	case QTUpsert:
		return trv.upsert.fil, trv.upsert.filNU
	case QTDelete:
		return trv.delete.fil, trv.delete.filNU
	}
	return nil, false
}

func (trv *trval) columnAllowed(qt *QCode, name string) bool {
	switch qt.SType {
	case QTQuery:
		_, ok := trv.query.cols[name]
		return ok || len(trv.query.cols) == 0
	case QTInsert:
		_, ok := trv.insert.cols[name]
		return ok || len(trv.insert.cols) == 0
	case QTUpdate:
		_, ok := trv.update.cols[name]
		return ok || len(trv.update.cols) == 0
	case QTUpsert:
		_, ok := trv.upsert.cols[name]
		return ok || len(trv.upsert.cols) == 0
	case QTDelete:
		_, ok := trv.delete.cols[name]
		return ok || len(trv.delete.cols) == 0
	}
	return false
}

func (trv *trval) limit(qt QType) int32 {
	if qt == QTQuery && trv.query.limit != 0 {
		return trv.query.limit
	}
	return 0
}

func (trv *trval) isBlocked(qt QType) bool {
	switch qt {
	case QTQuery:
		return trv.query.block
	case QTInsert:
		return trv.insert.block
	case QTUpdate:
		return trv.update.block
	case QTUpsert:
		return trv.upsert.block
	case QTDelete:
		return trv.delete.block
	}
	return false
}

func (trv *trval) isFuncsBlocked() bool {
	return trv.query.disable.funcs
}

// func (trv *trval) isMutationBlocked(mt MType, name string) error {
// 	var blocked bool
// 	switch mt {
// 	case MTInsert:
// 		blocked = trv.insert.block
// 	case MTUpdate:
// 		blocked = trv.update.block
// 	case MTUpset:
// 		blocked = trv.upsert.block
// 	case MTDelete:
// 		blocked = trv.upsert.block
// 	}

// 	if blocked {
// 		return fmt.Errorf("%s blocked: %s", item.Key)
// 	}
// 	return nil
// }

func (trv *trval) getPresets(mt MType) map[string]string {
	switch mt {
	case MTInsert:
		return trv.insert.presets
	case MTUpdate:
		return trv.update.presets
	case MTUpsert:
		return trv.upsert.presets
	}
	return nil
}

func makeSet(list []string) map[string]struct{} {
	m := make(map[string]struct{}, len(list))

	for i := range list {
		m[list[i]] = struct{}{}
	}
	return m
}

---
id: start-as-a-library
title: Getting Started as a library
sidebar_label: Use as a library
---

Super Graph can be used as a library in an already existing project.

The following code is just a simple example:

```go
package main

import (
	"database/sql"
	"github.com/dosco/super-graph/core"
	"github.com/go-chi/chi"
	"github.com/go-chi/render"
	_ "github.com/jackc/pgx/v4/stdlib"
)

func New(cfg *Cfg) *core.SuperGraph {
	dbConn, err := sql.Open("pgx", cfg.DB_URL)
	//check err

	superGraphConfig := NewConfig(cfg)

	supergraph, err := core.NewSuperGraph(&superGraphConfig, dbConn)
	//check err

	return supergraph
}

func Handler(superGraph *core.SuperGraph) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := getBodyFromRequest(w, r)
		//check err

		ctx := context.WithValue(r.Context(), core.UserIDKey, GetYourUserID())

    res, err := superGraph.GraphQL(ctx, body.Query, body.Variables)

		if err != nil {
			//check err
			return
		}

		render.JSON(w, r, res)
	}
}

func main() {
  superGraph := New(config)
  
  r := chi.NewRouter()
  
  r.Post("/api", Handler(superGraph))

  StartServer()
}
```
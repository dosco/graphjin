package serv

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dosco/graphjin/codesql"
	"github.com/dosco/graphjin/core/v3"
)

type codeSQLMutationAdapter struct {
	managed  *codesql.Managed
	readOnly bool
}

func (a codeSQLMutationAdapter) ManagedMutationTables() []string {
	return codesql.ManagedMutationTables()
}

func (a codeSQLMutationAdapter) ExecuteManagedMutation(ctx context.Context, req core.ManagedMutationRequest) (json.RawMessage, error) {
	if a.readOnly {
		return nil, fmt.Errorf("mutations blocked: database %s is read-only", req.Database)
	}
	roots := make([]codesql.MutationRoot, 0, len(req.Roots))
	for _, root := range req.Roots {
		fields := make([]codesql.MutationField, 0, len(root.Fields))
		for _, field := range root.Fields {
			fields = append(fields, codesql.MutationField{
				Name:   field.Name,
				Column: field.Column,
			})
		}
		roots = append(roots, codesql.MutationRoot{
			FieldName: root.FieldName,
			Table:     root.Table,
			Operation: root.Operation,
			Input:     root.Input,
			Fields:    fields,
		})
	}
	return a.managed.ExecuteManagedMutation(ctx, codesql.MutationRequest{
		Database:  req.Database,
		Operation: req.Operation,
		Roots:     roots,
	})
}

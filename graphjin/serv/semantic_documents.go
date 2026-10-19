package serv

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/dosco/graphjin/core/v3"
)

var semanticFacetOrder = []string{
	"identifiers",
	"measures",
	"time",
	"categorical/status",
	"text/search",
	"other",
}

func buildSemanticDocuments(metadata *core.MetadataSnapshot, snapshot *core.CatalogSnapshot) []semanticDocument {
	if metadata == nil || snapshot == nil {
		return nil
	}
	tableCards := make(map[string]string)
	columnCards := make(map[string]string)
	for _, card := range snapshot.Cards {
		switch card.Kind {
		case "table":
			tableCards[semanticTableKey(card.DatabaseName, card.SchemaName, card.TableName)] = card.ID
		case "column":
			columnCards[semanticColumnKey(card.DatabaseName, card.SchemaName, card.TableName, card.ColumnName)] = card.ID
		}
	}

	columnsByTable := make(map[string][]core.MetadataColumn)
	for _, column := range metadata.Columns {
		columnsByTable[column.TableID] = append(columnsByTable[column.TableID], column)
	}
	for key := range columnsByTable {
		sort.Slice(columnsByTable[key], func(a, b int) bool {
			left, right := columnsByTable[key][a], columnsByTable[key][b]
			if left.ColumnName != right.ColumnName {
				return left.ColumnName < right.ColumnName
			}
			if left.ID != right.ID {
				return left.ID < right.ID
			}
			return left.Ordinal < right.Ordinal
		})
	}

	relationshipsByTable := make(map[string][]core.MetadataRelationship)
	for _, relationship := range metadata.Relationships {
		from := semanticTableKey(relationship.FromDatabaseName, relationship.FromSchemaName, relationship.FromTableName)
		to := semanticTableKey(relationship.ToDatabaseName, relationship.ToSchemaName, relationship.ToTableName)
		relationshipsByTable[from] = append(relationshipsByTable[from], relationship)
		if to != from {
			relationshipsByTable[to] = append(relationshipsByTable[to], relationship)
		}
	}

	var documents []semanticDocument
	for _, table := range metadata.Tables {
		tableKey := semanticTableKey(table.DatabaseName, table.SchemaName, table.TableName)
		tableCard := tableCards[tableKey]
		if tableCard == "" {
			continue
		}
		qualified := semanticQualifiedName(table.DatabaseName, table.SchemaName, table.TableName)
		identity := fmt.Sprintf("table identity\nname: %s\ntype: %s", qualified, table.Type)
		if comment := strings.TrimSpace(table.Comment); comment != "" {
			identity += "\ncomment: " + comment
		}
		documents = append(documents, newSemanticDocument("table_identity", identity, []string{tableCard}, nil))

		facets := make(map[string][]core.MetadataColumn)
		for _, column := range columnsByTable[table.ID] {
			facet := semanticColumnFacet(column, relationshipsByTable[tableKey])
			facets[facet] = append(facets[facet], column)
		}
		for _, facet := range semanticFacetOrder {
			columns := facets[facet]
			for start := 0; start < len(columns); start += 64 {
				end := start + 64
				if end > len(columns) {
					end = len(columns)
				}
				chunk := columns[start:end]
				var text strings.Builder
				fmt.Fprintf(&text, "table column facet\ntable: %s\nfacet: %s\ncolumns:\n", qualified, facet)
				memberCards := make([]string, 0, len(chunk))
				for _, column := range chunk {
					flags := semanticColumnFlags(column, relationshipsByTable[tableKey])
					fmt.Fprintf(&text, "- %s type=%s", column.ColumnName, column.Type)
					if flags != "" {
						fmt.Fprintf(&text, " flags=%s", flags)
					}
					if comment := strings.TrimSpace(column.Comment); comment != "" {
						fmt.Fprintf(&text, " comment=%s", comment)
					}
					text.WriteByte('\n')
					if cardID := columnCards[semanticColumnKey(column.DatabaseName, column.SchemaName, column.TableName, column.ColumnName)]; cardID != "" {
						memberCards = append(memberCards, cardID)
					}
				}
				documents = append(documents, newSemanticDocument("column_facet", text.String(), []string{tableCard}, memberCards))
			}
		}

		var neighborhood strings.Builder
		fmt.Fprintf(&neighborhood, "relationship neighborhood\ntable: %s\nforeign keys:\n", qualified)
		targets := []string{tableCard}
		relationships := append([]core.MetadataRelationship(nil), relationshipsByTable[tableKey]...)
		sort.Slice(relationships, func(a, b int) bool { return relationships[a].ID < relationships[b].ID })
		for _, relationship := range relationships {
			from := semanticQualifiedName(relationship.FromDatabaseName, relationship.FromSchemaName, relationship.FromTableName)
			to := semanticQualifiedName(relationship.ToDatabaseName, relationship.ToSchemaName, relationship.ToTableName)
			fmt.Fprintf(&neighborhood, "- %s.%s -> %s.%s type=%s\n", from, relationship.FromColumnName, to, relationship.ToColumnName, relationship.RelType)
			for _, key := range []string{
				semanticTableKey(relationship.FromDatabaseName, relationship.FromSchemaName, relationship.FromTableName),
				semanticTableKey(relationship.ToDatabaseName, relationship.ToSchemaName, relationship.ToTableName),
			} {
				if cardID := tableCards[key]; cardID != "" {
					targets = appendUniqueSemantic(targets, cardID)
				}
			}
		}
		documents = append(documents, newSemanticDocument("relationship_neighborhood", neighborhood.String(), targets, nil))
	}

	for _, card := range snapshot.Cards {
		if !safeSemanticConceptCard(card) {
			continue
		}
		text := fmt.Sprintf("catalog concept\nkind: %s\ntitle: %s\nsummary: %s", card.Kind, card.Title, card.Summary)
		documents = append(documents, newSemanticDocument("concept", text, []string{card.ID}, nil))
	}

	sort.Slice(documents, func(a, b int) bool {
		if documents[a].Kind != documents[b].Kind {
			return documents[a].Kind < documents[b].Kind
		}
		return documents[a].Hash < documents[b].Hash
	})
	return documents
}

func newSemanticDocument(kind, text string, targets, members []string) semanticDocument {
	text = strings.TrimSpace(text)
	targets = sortedUniqueSemantic(targets)
	members = sortedUniqueSemantic(members)
	hashInput := fmt.Sprintf("semantic-document-v%d\nkind:%s\n%s", semanticDocumentFormatVersion, kind, text)
	sum := sha256.Sum256([]byte(hashInput))
	return semanticDocument{
		Hash:          hex.EncodeToString(sum[:]),
		Kind:          kind,
		Text:          text,
		TargetCardIDs: targets,
		MemberColumns: members,
	}
}

func semanticColumnFacet(column core.MetadataColumn, relationships []core.MetadataRelationship) string {
	name := strings.ToLower(column.ColumnName)
	typeName := strings.ToLower(column.Type)
	if column.PrimaryKey || column.UniqueKey || semanticColumnIsForeignKey(column, relationships) ||
		name == "id" || strings.HasSuffix(name, "_id") || strings.Contains(name, "uuid") || strings.HasSuffix(name, "_key") || strings.HasSuffix(name, "_code") {
		return "identifiers"
	}
	if semanticContainsAny(typeName, "date", "time") || semanticContainsAny(name, "date", "time", "month", "year", "created_at", "updated_at") {
		return "time"
	}
	if semanticContainsAny(name, "status", "state", "category", "type", "kind", "stage", "flag", "active", "enabled") ||
		semanticContainsAny(typeName, "bool", "enum") {
		return "categorical/status"
	}
	if semanticContainsAny(name, "amount", "revenue", "price", "cost", "total", "subtotal", "quantity", "qty", "count", "rate", "score", "balance") ||
		semanticContainsAny(typeName, "numeric", "decimal", "number", "float", "double", "real", "money", "int") {
		return "measures"
	}
	if semanticContainsAny(name, "name", "title", "description", "summary", "text", "body", "content", "search", "email") ||
		semanticContainsAny(typeName, "char", "text", "string") {
		return "text/search"
	}
	return "other"
}

func semanticColumnFlags(column core.MetadataColumn, relationships []core.MetadataRelationship) string {
	var flags []string
	if column.PrimaryKey {
		flags = append(flags, "primary_key")
	}
	if semanticColumnIsForeignKey(column, relationships) {
		flags = append(flags, "foreign_key")
	}
	if column.UniqueKey {
		flags = append(flags, "unique")
	}
	if column.Indexed {
		flags = append(flags, "indexed")
	}
	if column.NotNull {
		flags = append(flags, "not_null")
	}
	if column.Array {
		flags = append(flags, "array")
	}
	return strings.Join(flags, ",")
}

func semanticColumnIsForeignKey(column core.MetadataColumn, relationships []core.MetadataRelationship) bool {
	for _, relationship := range relationships {
		if relationship.FromColumnID == column.ID || relationship.ToColumnID == column.ID {
			return true
		}
	}
	return false
}

func safeSemanticConceptCard(card core.CatalogCard) bool {
	if card.Sensitive || strings.TrimSpace(card.Title) == "" || strings.TrimSpace(card.Summary) == "" {
		return false
	}
	switch card.Kind {
	case "database", "table", "column", "relationship", "index", "function":
		return false
	case "artifact", "user_artifact":
		return false
	case "workflow", "fragment", "saved_query":
		// Only shared/builtin cards are safe. Caller-owned overlay cards carry an
		// owner source and remain lexical-only.
		return card.OwnerSource == "" || strings.HasPrefix(card.Source, "builtin") || strings.HasPrefix(card.Source, "core.")
	default:
		return true
	}
}

func semanticQualifiedName(database, schema, name string) string {
	parts := make([]string, 0, 3)
	for _, value := range []string{database, schema, name} {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, ".")
}

func semanticTableKey(database, schema, table string) string {
	return strings.ToLower(strings.TrimSpace(database)) + ":" + strings.ToLower(strings.TrimSpace(schema)) + "." + strings.ToLower(strings.TrimSpace(table))
}

func semanticColumnKey(database, schema, table, column string) string {
	return semanticTableKey(database, schema, table) + "." + strings.ToLower(strings.TrimSpace(column))
}

func semanticContainsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func appendUniqueSemantic(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func sortedUniqueSemantic(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

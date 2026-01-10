package mongodriver

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// translateFieldName converts GraphQL field names to MongoDB field names.
// Most importantly, it maps "id" to "_id" since GraphQL convention uses "id"
// but MongoDB uses "_id" as the primary key.
func translateFieldName(name string) string {
	if name == "id" {
		return "_id"
	}
	return name
}

// translateFieldsInMap recursively translates field names in a map.
// This is used to convert filter/match/project stages to use MongoDB field names.
func translateFieldsInMap(m map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range m {
		newKey := translateFieldName(k)
		result[newKey] = translateFieldsInValue(v)
	}
	return result
}

// translateFieldsInValue recursively translates field names in any value.
func translateFieldsInValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return translateFieldsInMap(val)
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = translateFieldsInValue(item)
		}
		return result
	default:
		return v
	}
}

// translateIDFieldsBack converts MongoDB _id fields back to id for GraphQL response.
func translateIDFieldsBack(m bson.M) bson.M {
	result := make(bson.M)
	for k, v := range m {
		newKey := k
		// Translate _id back to id
		if k == "_id" {
			newKey = "id"
		}
		result[newKey] = translateIDValueBack(v)
	}
	return result
}

// translateIDValueBack recursively translates _id fields back in nested values.
func translateIDValueBack(v any) any {
	switch val := v.(type) {
	case bson.M:
		return translateIDFieldsBack(val)
	case bson.D:
		// bson.D is an ordered slice of key-value pairs
		result := make(bson.M)
		for _, elem := range val {
			newKey := elem.Key
			if elem.Key == "_id" {
				newKey = "id"
			}
			result[newKey] = translateIDValueBack(elem.Value)
		}
		return result
	case map[string]any:
		result := make(map[string]any)
		for k, vv := range val {
			newKey := k
			if k == "_id" {
				newKey = "id"
			}
			result[newKey] = translateIDValueBack(vv)
		}
		return result
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = translateIDValueBack(item)
		}
		return result
	case bson.A:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = translateIDValueBack(item)
		}
		return result
	default:
		return v
	}
}

// normalizeID converts numeric IDs to int64 for consistent map key comparison.
// This handles type differences between JSON parsing (float64) and MongoDB (int32/int64).
func normalizeID(id any) any {
	switch v := id.(type) {
	case float64:
		return int64(v)
	case float32:
		return int64(v)
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case uint:
		return int64(v)
	case uint32:
		return int64(v)
	case uint64:
		return int64(v)
	default:
		return id
	}
}

// extractProjectedFields extracts field names from $project stages in a pipeline.
// Used to add null values for fields that were requested but don't exist in the document.
func extractProjectedFields(pipeline []map[string]any) []string {
	var fields []string
	for _, stage := range pipeline {
		if project, ok := stage["$project"].(map[string]any); ok {
			for field := range project {
				// Skip _id as it's handled separately and translate to id
				if field == "_id" {
					fields = append(fields, "id")
				} else {
					fields = append(fields, field)
				}
			}
		}
	}
	return fields
}

// executeAggregate runs an aggregation pipeline.
func (c *Conn) executeAggregate(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	if q.Collection == "" {
		return nil, fmt.Errorf("mongodriver: aggregate requires collection")
	}

	coll := c.db.Collection(q.Collection)

	// Convert pipeline to bson.A, translating field names (id -> _id)
	pipeline := make(bson.A, len(q.Pipeline))
	for i, stage := range q.Pipeline {
		pipeline[i] = translateFieldsInMap(stage)
	}

	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: aggregate: %w", err)
	}

	// Collect all results into a JSON array
	var results []bson.M
	if err := cursor.All(ctx, &results); err != nil {
		cursor.Close(ctx)
		return nil, fmt.Errorf("mongodriver: aggregate results: %w", err)
	}
	cursor.Close(ctx)

	// Transform _id to id in results for GraphQL convention
	for i := range results {
		results[i] = translateIDFieldsBack(results[i])
	}

	// Wrap results in field name and handle singular vs plural
	var finalResult any
	if q.Singular {
		// For singular queries, return first result or null
		if len(results) > 0 {
			finalResult = map[string]any{q.FieldName: results[0]}
		} else {
			finalResult = map[string]any{q.FieldName: nil}
		}
	} else {
		// For plural queries, return array
		finalResult = map[string]any{q.FieldName: results}
	}

	jsonBytes, err := json.Marshal(finalResult)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: marshal results: %w", err)
	}

	return NewSingleValueRows(jsonBytes, []string{"__root"}), nil
}

// executeMultiAggregate runs multiple aggregation pipelines and merges results.
// This is used for multi-root GraphQL queries where each root queries a different collection.
func (c *Conn) executeMultiAggregate(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	if len(q.Queries) == 0 {
		return nil, fmt.Errorf("mongodriver: multi_aggregate requires queries array")
	}

	// Merge all results into a single map
	finalResult := make(map[string]any)

	for _, subQ := range q.Queries {
		if subQ.Collection == "" {
			return nil, fmt.Errorf("mongodriver: aggregate requires collection")
		}

		coll := c.db.Collection(subQ.Collection)

		// Convert pipeline to bson.A, translating field names (id -> _id)
		pipeline := make(bson.A, len(subQ.Pipeline))
		for i, stage := range subQ.Pipeline {
			pipeline[i] = translateFieldsInMap(stage)
		}

		cursor, err := coll.Aggregate(ctx, pipeline)
		if err != nil {
			return nil, fmt.Errorf("mongodriver: aggregate on %s: %w", subQ.Collection, err)
		}

		// Collect all results
		var results []bson.M
		if err := cursor.All(ctx, &results); err != nil {
			cursor.Close(ctx)
			return nil, fmt.Errorf("mongodriver: aggregate results on %s: %w", subQ.Collection, err)
		}
		cursor.Close(ctx)

		// Transform _id to id in results for GraphQL convention
		for i := range results {
			results[i] = translateIDFieldsBack(results[i])
		}

		// Add to final result under the field name
		if subQ.Singular {
			if len(results) > 0 {
				finalResult[subQ.FieldName] = results[0]
			} else {
				finalResult[subQ.FieldName] = nil
			}
		} else {
			finalResult[subQ.FieldName] = results
		}
	}

	jsonBytes, err := json.Marshal(finalResult)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: marshal multi results: %w", err)
	}

	return NewSingleValueRows(jsonBytes, []string{"__root"}), nil
}

// executeFind runs a find query.
func (c *Conn) executeFind(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	if q.Collection == "" {
		return nil, fmt.Errorf("mongodriver: find requires collection")
	}

	coll := c.db.Collection(q.Collection)

	filter := bson.M{}
	if q.Filter != nil {
		// Translate field names (id -> _id)
		filter = translateFieldsInMap(q.Filter)
	}

	findOpts := options.Find()
	if q.Options != nil {
		if limit, ok := q.Options["limit"].(float64); ok {
			findOpts.SetLimit(int64(limit))
		}
		if skip, ok := q.Options["skip"].(float64); ok {
			findOpts.SetSkip(int64(skip))
		}
		if sort, ok := q.Options["sort"].(map[string]any); ok {
			findOpts.SetSort(sort)
		}
		if projection, ok := q.Options["projection"].(map[string]any); ok {
			findOpts.SetProjection(projection)
		}
	}

	cursor, err := coll.Find(ctx, filter, findOpts)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: find: %w", err)
	}

	// Collect all results
	var results []bson.M
	if err := cursor.All(ctx, &results); err != nil {
		cursor.Close(ctx)
		return nil, fmt.Errorf("mongodriver: find results: %w", err)
	}
	cursor.Close(ctx)

	jsonBytes, err := json.Marshal(results)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: marshal results: %w", err)
	}

	return NewSingleValueRows(jsonBytes, []string{"__root"}), nil
}

// executeFindOne runs a findOne query.
func (c *Conn) executeFindOne(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	if q.Collection == "" {
		return nil, fmt.Errorf("mongodriver: findOne requires collection")
	}

	coll := c.db.Collection(q.Collection)

	filter := bson.M{}
	if q.Filter != nil {
		// Translate field names (id -> _id)
		filter = translateFieldsInMap(q.Filter)
	}

	var result bson.M
	err := coll.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return NewSingleValueRows([]byte("null"), []string{"__root"}), nil
		}
		return nil, fmt.Errorf("mongodriver: findOne: %w", err)
	}

	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: marshal result: %w", err)
	}

	return NewSingleValueRows(jsonBytes, []string{"__root"}), nil
}

// executeInsertOne inserts a single document.
func (c *Conn) executeInsertOne(ctx context.Context, q *QueryDSL) (driver.Result, error) {
	if q.Collection == "" {
		return nil, fmt.Errorf("mongodriver: insertOne requires collection")
	}
	if q.Document == nil {
		return nil, fmt.Errorf("mongodriver: insertOne requires document")
	}

	coll := c.db.Collection(q.Collection)
	result, err := coll.InsertOne(ctx, q.Document)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: insertOne: %w", err)
	}

	return &Result{
		lastInsertID: fmt.Sprintf("%v", result.InsertedID),
		rowsAffected: 1,
	}, nil
}

// executeInsertOneAsQuery inserts a document and returns it as query results.
// This is used when GraphQL mutations need to return the inserted data.
func (c *Conn) executeInsertOneAsQuery(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	if q.Collection == "" {
		return nil, fmt.Errorf("mongodriver: insertOne requires collection")
	}
	if q.Document == nil {
		return nil, fmt.Errorf("mongodriver: insertOne requires document")
	}

	// Translate field names in document (id -> _id)
	doc := translateFieldsInMap(q.Document)

	// Merge presets into document (presets override user-provided values)
	if q.Presets != nil {
		for k, v := range q.Presets {
			doc[k] = v
		}
	}

	coll := c.db.Collection(q.Collection)
	result, err := coll.InsertOne(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: insertOne: %w", err)
	}

	var finalDoc bson.M

	// If there's a return_pipeline, use aggregate to fetch the inserted document with related data
	if len(q.ReturnPipeline) > 0 {
		// Build pipeline: $match the inserted document, then apply return_pipeline
		pipeline := make(bson.A, 0, len(q.ReturnPipeline)+1)

		// First stage: match the inserted document
		pipeline = append(pipeline, bson.M{"$match": bson.M{"_id": result.InsertedID}})

		// Add return pipeline stages
		for _, stage := range q.ReturnPipeline {
			pipeline = append(pipeline, translateFieldsInMap(stage))
		}

		cursor, err := coll.Aggregate(ctx, pipeline)
		if err != nil {
			return nil, fmt.Errorf("mongodriver: aggregate after insert: %w", err)
		}

		var results []bson.M
		if err := cursor.All(ctx, &results); err != nil {
			cursor.Close(ctx)
			return nil, fmt.Errorf("mongodriver: aggregate results: %w", err)
		}
		cursor.Close(ctx)

		if len(results) > 0 {
			finalDoc = results[0]
		} else {
			// Fallback to simple findOne
			err = coll.FindOne(ctx, bson.M{"_id": result.InsertedID}).Decode(&finalDoc)
			if err != nil {
				return nil, fmt.Errorf("mongodriver: findOne after insert: %w", err)
			}
		}
	} else {
		// No return_pipeline, just fetch the inserted document
		err = coll.FindOne(ctx, bson.M{"_id": result.InsertedID}).Decode(&finalDoc)
		if err != nil {
			return nil, fmt.Errorf("mongodriver: findOne after insert: %w", err)
		}
	}

	// Translate _id back to id
	finalDoc = translateIDFieldsBack(finalDoc)

	// Wrap result in field name if provided
	var finalResult any
	if q.FieldName != "" {
		finalResult = map[string]any{q.FieldName: []any{finalDoc}}
	} else {
		finalResult = []any{finalDoc}
	}

	jsonBytes, err := json.Marshal(finalResult)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: marshal insert result: %w", err)
	}

	return NewSingleValueRows(jsonBytes, []string{"__root"}), nil
}

// executeInsertMany inserts multiple documents.
func (c *Conn) executeInsertMany(ctx context.Context, q *QueryDSL) (driver.Result, error) {
	if q.Collection == "" {
		return nil, fmt.Errorf("mongodriver: insertMany requires collection")
	}

	// Get documents from q.Documents (bulk insert via raw_document array) or q.Options["documents"]
	var docs []any
	if len(q.Documents) > 0 {
		// Translate id -> _id for each document
		docs = make([]any, len(q.Documents))
		for i, doc := range q.Documents {
			docs[i] = translateFieldsInMap(doc)
		}
	} else if optDocs, ok := q.Options["documents"].([]any); ok && len(optDocs) > 0 {
		docs = optDocs
	} else {
		return nil, fmt.Errorf("mongodriver: insertMany requires documents array")
	}

	coll := c.db.Collection(q.Collection)
	result, err := coll.InsertMany(ctx, docs)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: insertMany: %w", err)
	}

	return &Result{
		rowsAffected: int64(len(result.InsertedIDs)),
	}, nil
}

// executeInsertManyAsQuery inserts multiple documents and returns them as query results.
// This is used when GraphQL mutations need to return the inserted data.
func (c *Conn) executeInsertManyAsQuery(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	if q.Collection == "" {
		return nil, fmt.Errorf("mongodriver: insertMany requires collection")
	}

	// Get documents from q.Documents (bulk insert via raw_document array) or q.Options["documents"]
	var docs []any
	if len(q.Documents) > 0 {
		// Translate id -> _id for each document
		docs = make([]any, len(q.Documents))
		for i, doc := range q.Documents {
			docs[i] = translateFieldsInMap(doc)
		}
	} else if optDocs, ok := q.Options["documents"].([]any); ok && len(optDocs) > 0 {
		docs = optDocs
	} else {
		return nil, fmt.Errorf("mongodriver: insertMany requires documents array")
	}

	coll := c.db.Collection(q.Collection)
	result, err := coll.InsertMany(ctx, docs)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: insertMany: %w", err)
	}

	// Fetch all inserted documents
	var finalDocs []bson.M
	if len(q.ReturnPipeline) > 0 {
		// Use aggregation to fetch inserted documents with related data
		pipeline := make(bson.A, 0, len(q.ReturnPipeline)+1)

		// First stage: match all inserted documents
		pipeline = append(pipeline, bson.M{"$match": bson.M{"_id": bson.M{"$in": result.InsertedIDs}}})

		// Add return pipeline stages
		for _, stage := range q.ReturnPipeline {
			pipeline = append(pipeline, translateFieldsInMap(stage))
		}

		cursor, err := coll.Aggregate(ctx, pipeline)
		if err != nil {
			return nil, fmt.Errorf("mongodriver: aggregate after insertMany: %w", err)
		}

		if err := cursor.All(ctx, &finalDocs); err != nil {
			cursor.Close(ctx)
			return nil, fmt.Errorf("mongodriver: aggregate results: %w", err)
		}
		cursor.Close(ctx)
	} else {
		// No return_pipeline, just fetch the inserted documents
		cursor, err := coll.Find(ctx, bson.M{"_id": bson.M{"$in": result.InsertedIDs}})
		if err != nil {
			return nil, fmt.Errorf("mongodriver: find after insertMany: %w", err)
		}

		if err := cursor.All(ctx, &finalDocs); err != nil {
			cursor.Close(ctx)
			return nil, fmt.Errorf("mongodriver: cursor results: %w", err)
		}
		cursor.Close(ctx)
	}

	// Translate _id back to id for all documents
	resultDocs := make([]any, len(finalDocs))
	for i, doc := range finalDocs {
		resultDocs[i] = translateIDFieldsBack(doc)
	}

	// Wrap result in field name if provided
	var finalResult any
	if q.FieldName != "" {
		finalResult = map[string]any{q.FieldName: resultDocs}
	} else {
		finalResult = resultDocs
	}

	jsonBytes, err := json.Marshal(finalResult)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: marshal insertMany result: %w", err)
	}

	return NewSingleValueRows(jsonBytes, []string{"__root"}), nil
}

// executeNestedInsert handles inserting documents into multiple related collections.
// It executes inserts in topological order based on dependencies and links FK values.
func (c *Conn) executeNestedInsert(ctx context.Context, q *QueryDSL) (driver.Rows, error) {
	if len(q.Inserts) == 0 {
		return nil, fmt.Errorf("mongodriver: nested_insert requires inserts array")
	}
	if q.RootCollection == "" {
		return nil, fmt.Errorf("mongodriver: nested_insert requires root_collection")
	}

	// Map to track inserted IDs for FK linking
	insertedIDs := make(map[int]any) // mutation ID -> inserted _id

	// Build a map of parent ID -> list of child inserts
	parentToChildren := make(map[int][]*NestedInsert)
	for i := range q.Inserts {
		ins := &q.Inserts[i]
		if ins.ParentID != -1 {
			parentToChildren[ins.ParentID] = append(parentToChildren[ins.ParentID], ins)
		}
	}

	// Execute inserts in order (already topologically sorted by dialect)
	for _, ins := range q.Inserts {
		coll := c.db.Collection(ins.Collection)

		// Handle connect operations as updates to existing documents
		if ins.IsConnect {
			// For connect operations, UPDATE the existing document to set its FK
			// Get the document ID to update - MongoDB uses _id as the key
			docID := ins.Document["id"]
			if docID == nil {
				docID = ins.Document["_id"]
			}
			if docID == nil {
				return nil, fmt.Errorf("mongodriver: connect operation missing document id")
			}

			// Get the parent's inserted ID to set as the FK value
			parentInsertedID, hasParent := insertedIDs[ins.ParentID]
			if !hasParent || parentInsertedID == nil {
				return nil, fmt.Errorf("mongodriver: connect operation missing parent id")
			}

			// Update the existing document's FK column
			// Note: docID is used directly as MongoDB _id can be any type
			filter := bson.M{"_id": docID}
			update := bson.M{"$set": bson.M{ins.FKCol: parentInsertedID}}
			_, err := coll.UpdateOne(ctx, filter, update)
			if err != nil {
				return nil, fmt.Errorf("mongodriver: connect update %s: %w", ins.Collection, err)
			}

			// Store the connected document's ID for any further FK linking
			insertedIDs[ins.ID] = docID
			continue
		}

		doc := translateFieldsInMap(ins.Document)

		// For root document, apply FK values (direct FK values from connect operations)
		if ins.ID == q.RootMutateID && len(q.FKValues) > 0 {
			for col, val := range q.FKValues {
				doc[col] = val
			}
		}

		// For root document, apply FK connects transformation
		// FK connects transform paths like product.connect.id -> product_id
		if ins.ID == q.RootMutateID && len(q.FKConnects) > 0 {
			for _, fkc := range q.FKConnects {
				transformFKConnect(doc, fkc.Path, fkc.Column)
			}
		}

		// Check if this insert needs to set FK from a child that was already inserted
		// This happens when FK is on parent (e.g., products.owner_id = users._id)
		for _, childIns := range parentToChildren[ins.ID] {
			if childIns.FKOnParent && childIns.FKCol != "" {
				// FK is on THIS table (parent), child was inserted first
				childInsertedID, hasChild := insertedIDs[childIns.ID]
				if hasChild && childInsertedID != nil {
					doc[childIns.FKCol] = childInsertedID
				}
			}
		}

		// Check if this insert needs to set FK from parent that was already inserted
		// This happens when FK is on child (e.g., products.owner_id when products is child of users)
		if ins.ParentID != -1 && !ins.FKOnParent && ins.FKCol != "" {
			// FK is on THIS table (child), parent was inserted first
			parentInsertedID, hasParent := insertedIDs[ins.ParentID]
			if hasParent && parentInsertedID != nil {
				doc[ins.FKCol] = parentInsertedID
			}
		}

		result, err := coll.InsertOne(ctx, doc)
		if err != nil {
			return nil, fmt.Errorf("mongodriver: nested insert into %s: %w", ins.Collection, err)
		}

		insertedIDs[ins.ID] = result.InsertedID
	}

	// Get the root mutation ID to find the root document
	rootID := insertedIDs[q.RootMutateID]
	if rootID == nil {
		return nil, fmt.Errorf("mongodriver: root mutation ID %d not found in inserted IDs", q.RootMutateID)
	}

	var finalResult any

	// For recursive-only mutations (all inserts in same collection),
	// return ALL inserted/connected documents as an array
	if q.AllSameCollection {
		// Collect all inserted IDs
		allIDs := make([]any, 0, len(insertedIDs))
		for _, id := range insertedIDs {
			allIDs = append(allIDs, id)
		}

		coll := c.db.Collection(q.RootCollection)
		var results []bson.M

		// Use aggregate with return_pipeline if available (for field projection)
		if len(q.ReturnPipeline) > 0 {
			pipeline := make(bson.A, 0, len(q.ReturnPipeline)+1)
			// Match all inserted/connected documents
			pipeline = append(pipeline, bson.M{"$match": bson.M{"_id": bson.M{"$in": allIDs}}})
			// Add return pipeline stages (especially $project for field selection)
			for _, stage := range q.ReturnPipeline {
				pipeline = append(pipeline, translateFieldsInMap(stage))
			}

			cursor, err := coll.Aggregate(ctx, pipeline)
			if err != nil {
				return nil, fmt.Errorf("mongodriver: aggregate all inserted docs: %w", err)
			}

			if err := cursor.All(ctx, &results); err != nil {
				cursor.Close(ctx)
				return nil, fmt.Errorf("mongodriver: cursor all: %w", err)
			}
			cursor.Close(ctx)
		} else {
			// No return_pipeline, just fetch all documents
			cursor, err := coll.Find(ctx, bson.M{"_id": bson.M{"$in": allIDs}})
			if err != nil {
				return nil, fmt.Errorf("mongodriver: find all inserted docs: %w", err)
			}

			if err := cursor.All(ctx, &results); err != nil {
				cursor.Close(ctx)
				return nil, fmt.Errorf("mongodriver: cursor all: %w", err)
			}
			cursor.Close(ctx)
		}

		// Build a map for quick lookup by ID
		// Use normalizeID to handle type differences (e.g., float64 from JSON vs int32/int64 from MongoDB)
		resultMap := make(map[any]bson.M)
		for _, doc := range results {
			if id, ok := doc["_id"]; ok {
				resultMap[normalizeID(id)] = doc
			}
		}

		// Extract projected fields from return_pipeline to add nulls for missing fields
		projectedFields := extractProjectedFields(q.ReturnPipeline)

		// Reorder results to match insertion order (parent first, then children)
		// and translate _id back to id
		allDocs := make([]any, 0, len(results))
		for _, ins := range q.Inserts {
			id := normalizeID(insertedIDs[ins.ID])
			if doc, ok := resultMap[id]; ok {
				translatedDoc := translateIDFieldsBack(doc)
				// Add null for missing projected fields
				for _, field := range projectedFields {
					if _, exists := translatedDoc[field]; !exists {
						translatedDoc[field] = nil
					}
				}
				allDocs = append(allDocs, translatedDoc)
			}
		}

		// Wrap result in field name as array
		if q.FieldName != "" {
			finalResult = map[string]any{q.FieldName: allDocs}
		} else {
			finalResult = allDocs
		}
	} else {
		// Standard case: return single root document with nested lookups
		var finalDoc bson.M

		// Run return_pipeline on root collection to fetch all related data
		if len(q.ReturnPipeline) > 0 {
			pipeline := make(bson.A, 0, len(q.ReturnPipeline)+1)

			// First stage: match the root document
			pipeline = append(pipeline, bson.M{"$match": bson.M{"_id": rootID}})

			// Add return pipeline stages ($lookup, $project, etc.)
			for _, stage := range q.ReturnPipeline {
				pipeline = append(pipeline, translateFieldsInMap(stage))
			}

			coll := c.db.Collection(q.RootCollection)
			cursor, err := coll.Aggregate(ctx, pipeline)
			if err != nil {
				return nil, fmt.Errorf("mongodriver: aggregate after nested insert: %w", err)
			}

			var results []bson.M
			if err := cursor.All(ctx, &results); err != nil {
				cursor.Close(ctx)
				return nil, fmt.Errorf("mongodriver: aggregate results: %w", err)
			}
			cursor.Close(ctx)

			if len(results) > 0 {
				finalDoc = results[0]
			}
		} else {
			// No return_pipeline, just fetch the root document
			coll := c.db.Collection(q.RootCollection)
			err := coll.FindOne(ctx, bson.M{"_id": rootID}).Decode(&finalDoc)
			if err != nil {
				return nil, fmt.Errorf("mongodriver: findOne after nested insert: %w", err)
			}
		}

		// Translate _id back to id
		finalDoc = translateIDFieldsBack(finalDoc)

		// Wrap result in field name (as object if singular, array otherwise)
		if q.FieldName != "" {
			if q.Singular {
				finalResult = map[string]any{q.FieldName: finalDoc}
			} else {
				finalResult = map[string]any{q.FieldName: []any{finalDoc}}
			}
		} else {
			if q.Singular {
				finalResult = finalDoc
			} else {
				finalResult = []any{finalDoc}
			}
		}
	}

	jsonBytes, err := json.Marshal(finalResult)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: marshal nested insert result: %w", err)
	}

	return NewSingleValueRows(jsonBytes, []string{"__root"}), nil
}

// executeUpdateOne updates a single document.
func (c *Conn) executeUpdateOne(ctx context.Context, q *QueryDSL) (driver.Result, error) {
	if q.Collection == "" {
		return nil, fmt.Errorf("mongodriver: updateOne requires collection")
	}

	filter := bson.M{}
	if q.Filter != nil {
		// Translate field names (id -> _id)
		filter = translateFieldsInMap(q.Filter)
	}

	update := bson.M{}
	if q.Update != nil {
		// Translate field names in update as well
		update = translateFieldsInMap(q.Update)
	}

	coll := c.db.Collection(q.Collection)

	updateOpts := options.UpdateOne()
	if q.Options != nil {
		if upsert, ok := q.Options["upsert"].(bool); ok && upsert {
			updateOpts.SetUpsert(true)
		}
	}

	result, err := coll.UpdateOne(ctx, filter, update, updateOpts)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: updateOne: %w", err)
	}

	affected := result.MatchedCount
	if result.UpsertedCount > 0 {
		affected = result.UpsertedCount
	}

	return &Result{
		rowsAffected: affected,
	}, nil
}

// executeUpdateMany updates multiple documents.
func (c *Conn) executeUpdateMany(ctx context.Context, q *QueryDSL) (driver.Result, error) {
	if q.Collection == "" {
		return nil, fmt.Errorf("mongodriver: updateMany requires collection")
	}

	filter := bson.M{}
	if q.Filter != nil {
		// Translate field names (id -> _id)
		filter = translateFieldsInMap(q.Filter)
	}

	update := bson.M{}
	if q.Update != nil {
		// Translate field names in update as well
		update = translateFieldsInMap(q.Update)
	}

	coll := c.db.Collection(q.Collection)
	result, err := coll.UpdateMany(ctx, filter, update)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: updateMany: %w", err)
	}

	return &Result{
		rowsAffected: result.ModifiedCount,
	}, nil
}

// executeDeleteOne deletes a single document.
func (c *Conn) executeDeleteOne(ctx context.Context, q *QueryDSL) (driver.Result, error) {
	if q.Collection == "" {
		return nil, fmt.Errorf("mongodriver: deleteOne requires collection")
	}

	filter := bson.M{}
	if q.Filter != nil {
		// Translate field names (id -> _id)
		filter = translateFieldsInMap(q.Filter)
	}

	coll := c.db.Collection(q.Collection)
	result, err := coll.DeleteOne(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: deleteOne: %w", err)
	}

	return &Result{
		rowsAffected: result.DeletedCount,
	}, nil
}

// executeDeleteMany deletes multiple documents.
func (c *Conn) executeDeleteMany(ctx context.Context, q *QueryDSL) (driver.Result, error) {
	if q.Collection == "" {
		return nil, fmt.Errorf("mongodriver: deleteMany requires collection")
	}

	filter := bson.M{}
	if q.Filter != nil {
		// Translate field names (id -> _id)
		filter = translateFieldsInMap(q.Filter)
	}

	coll := c.db.Collection(q.Collection)
	result, err := coll.DeleteMany(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("mongodriver: deleteMany: %w", err)
	}

	return &Result{
		rowsAffected: result.DeletedCount,
	}, nil
}

// Result implements driver.Result.
type Result struct {
	lastInsertID string
	rowsAffected int64
}

// LastInsertId returns the ID of the last inserted document.
func (r *Result) LastInsertId() (int64, error) {
	// MongoDB uses ObjectIDs, not integers
	return 0, fmt.Errorf("mongodriver: LastInsertId not supported, use string ID")
}

// RowsAffected returns the number of affected rows.
func (r *Result) RowsAffected() (int64, error) {
	return r.rowsAffected, nil
}

// InsertedID returns the inserted document ID as a string.
func (r *Result) InsertedID() string {
	return r.lastInsertID
}

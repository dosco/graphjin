package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Worlds described in a sentence.
//
// The three built-in vocabularies are ordinary businesses on purpose, but they
// are three. A lab training on genome sequencing, chemical manufacturing or
// clinical trials needs a world with that vocabulary, and nobody should have to
// send a pull request to get one.
//
// So a model is asked to describe the business once, and its answer is saved
// into the world directory as world-pack.json. From then on the world is
// reproduced from that file, deterministically, with no model and no spend. The
// description is the artifact; the model is only how the artifact got written.

const worldPackSchemaVersion = "graphjin.world-pack/v1"

// worldPackFile is what a model returns and what a person can hand-write.
type worldPackFile struct {
	AppName    string            `json:"app_name"`
	DomainSlug string            `json:"domain_slug"`
	Entities   []worldPackEntity `json:"entities"`
}

type worldPackEntity struct {
	Table    string   `json:"table"`
	Label    string   `json:"label"`
	Metric   string   `json:"metric"`
	Date     string   `json:"date"`
	Statuses []string `json:"statuses"`
	Follows  string   `json:"follows,omitempty"`
}

// worldPackEnvelope is the saved form: the description, who wrote it, and the
// pack itself. Keeping the prompt beside the answer is what makes a surprising
// world explainable six months later.
type worldPackEnvelope struct {
	SchemaVersion string        `json:"schema_version"`
	Described     string        `json:"described,omitempty"`
	AuthoredBy    string        `json:"authored_by,omitempty"`
	Pack          worldPackFile `json:"pack"`
}

const worldPackSignature = `"You are describing a real business so a database can be built for it. Given a short description of an organization, name the records it would actually keep. Return between two and eight entities, ordered so that anything referencing another comes after it. For each entity give: table, a plural lowercase snake_case table name; label, the column holding its human name or reference; metric, a column holding a number worth aggregating; date, a column holding when it happened; statuses, between two and six lowercase snake_case states that entity moves through; and optionally follows, the table of an earlier entity this one belongs to. Use the vocabulary the industry actually uses. Do not use 'id' or 'status' as a label, metric or date — those are added for you. Reply with only a JSON object."
description:string "The organization to describe.",
max_tables:string "The most entities to return."
-> pack_json:string "JSON object {app_name, domain_slug, entities: [{table, label, metric, date, statuses, follows}]}."`

var (
	worldPackIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]{2,48}$`)
	worldPackSlug       = regexp.MustCompile(`^[a-z][a-z0-9-]{1,32}$`)
)

// validateWorldPack turns a described business into a domain vocabulary, or
// says exactly what was wrong with it.
//
// Every rejection names the field and the value. A model that returns something
// unusable is ordinary; a model that returns something unusable and gets a
// generic parse error wastes the next hour of somebody's afternoon.
func validateWorldPack(pack worldPackFile) (domainPack, error) {
	appName := strings.TrimSpace(pack.AppName)
	if appName == "" || len(appName) > 60 {
		return domainPack{}, fmt.Errorf("app_name must be a short name, got %q", pack.AppName)
	}
	slug := strings.TrimSpace(pack.DomainSlug)
	if !worldPackSlug.MatchString(slug) {
		return domainPack{}, fmt.Errorf("domain_slug %q must be lowercase words joined by hyphens", pack.DomainSlug)
	}
	if len(pack.Entities) < 2 || len(pack.Entities) > 8 {
		return domainPack{}, fmt.Errorf("a world needs between 2 and 8 entities, got %d", len(pack.Entities))
	}

	out := domainPack{Name: slug, AppName: appName}
	placed := map[string]bool{}
	for index, entity := range pack.Entities {
		where := fmt.Sprintf("entity %d (%s)", index+1, entity.Table)
		for field, value := range map[string]string{
			"table": entity.Table, "label": entity.Label, "metric": entity.Metric, "date": entity.Date,
		} {
			if !worldPackIdentifier.MatchString(value) {
				return domainPack{}, fmt.Errorf("%s: %s %q must be a lowercase snake_case name", where, field, value)
			}
			if strings.HasPrefix(value, "gj_") {
				return domainPack{}, fmt.Errorf("%s: %s %q uses a reserved prefix", where, field, value)
			}
		}
		if placed[entity.Table] {
			return domainPack{}, fmt.Errorf("%s: table %q appears twice", where, entity.Table)
		}
		// id and status are added to every table, so an entity naming one of them
		// would declare the same column twice and fail to create.
		seen := map[string]bool{"id": true, "status": true}
		for field, value := range map[string]string{"label": entity.Label, "metric": entity.Metric, "date": entity.Date} {
			if seen[value] {
				return domainPack{}, fmt.Errorf("%s: %s %q is already a column of every table", where, field, value)
			}
			seen[value] = true
		}
		if len(entity.Statuses) < 2 || len(entity.Statuses) > 6 {
			return domainPack{}, fmt.Errorf("%s: needs between 2 and 6 statuses, got %d", where, len(entity.Statuses))
		}
		states := map[string]bool{}
		for _, status := range entity.Statuses {
			if !worldPackIdentifier.MatchString(status) {
				return domainPack{}, fmt.Errorf("%s: status %q must be a lowercase snake_case state", where, status)
			}
			if states[status] {
				return domainPack{}, fmt.Errorf("%s: status %q appears twice", where, status)
			}
			states[status] = true
		}
		// Ordering is the contract the renderer relies on: a table is created
		// after whatever it references, and its seeded foreign keys are drawn
		// from rows that already exist.
		if follows := strings.TrimSpace(entity.Follows); follows != "" && !placed[follows] {
			return domainPack{}, fmt.Errorf(
				"%s: follows %q, which is not one of the entities listed before it", where, follows)
		}
		placed[entity.Table] = true
		out.Roots = append(out.Roots, worldEntity{
			Table: entity.Table, Label: entity.Label, Metric: entity.Metric,
			Date: entity.Date, Status: entity.Statuses, Follows: strings.TrimSpace(entity.Follows),
		})
	}
	return out, nil
}

// loadWorldPack reads a saved description. It accepts the envelope this writes
// and a bare pack somebody wrote by hand, because a world description is a
// reasonable thing to author directly.
//
// The description and its author come back with it, so a world rebuilt from a
// pack still records who wrote the vocabulary rather than losing that at the
// first rebuild.
func loadWorldPack(path string) (worldPackEnvelope, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return worldPackEnvelope{}, err
	}
	var envelope worldPackEnvelope
	if err := json.Unmarshal(body, &envelope); err == nil && len(envelope.Pack.Entities) != 0 {
		return envelope, nil
	}
	var pack worldPackFile
	if err := json.Unmarshal(body, &pack); err != nil {
		return worldPackEnvelope{}, fmt.Errorf("%s is not a world description: %w", path, err)
	}
	if len(pack.Entities) == 0 {
		return worldPackEnvelope{}, fmt.Errorf("%s describes no entities", path)
	}
	return worldPackEnvelope{Pack: pack}, nil
}

// writeWorldPackFile saves the description inside the world it produced, so the
// world can be rebuilt without asking a model again.
func writeWorldPackFile(directory, name string, envelope worldPackEnvelope) error {
	envelope.SchemaVersion = worldPackSchemaVersion
	body, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(directory, name), append(body, '\n'), 0o600)
}

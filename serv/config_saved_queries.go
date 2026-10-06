package serv

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// savedQueryUpdate is a saved query that a gj_config update writes to the
// config folder. GraphJin reads saved queries from queries/<name>.gql, so a
// remote client can add them without access to the server's files.
type savedQueryUpdate struct {
	name  string
	query string
}

var savedQueryNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// parseSavedQueryUpdates reads update_saved_queries and remove_saved_queries.
// Query text is not compiled here: it often uses a source that the same
// update adds. GraphJin compiles each saved query when it runs.
func parseSavedQueryUpdates(args map[string]any) (writes []savedQueryUpdate, removals []string, changes, errs []string) {
	if raw, ok := args["update_saved_queries"]; ok {
		items, ok := raw.([]any)
		if !ok {
			errs = append(errs, "update_saved_queries must be a list of {name, query} objects")
		}
		for i, item := range items {
			entry, ok := item.(map[string]any)
			if !ok {
				errs = append(errs, fmt.Sprintf("update_saved_queries[%d] must be an object with name and query", i))
				continue
			}
			name, _ := entry["name"].(string)
			query, _ := entry["query"].(string)
			name = strings.TrimSpace(name)
			switch {
			case !savedQueryNameRe.MatchString(name):
				errs = append(errs, fmt.Sprintf("update_saved_queries[%d].name %q must use only letters, numbers, underscores and dashes", i, name))
			case strings.TrimSpace(query) == "":
				errs = append(errs, fmt.Sprintf("update_saved_queries[%d].query for %s is empty", i, name))
			default:
				writes = append(writes, savedQueryUpdate{name: name, query: strings.TrimSpace(query)})
				changes = append(changes, "updated saved query: "+name)
			}
		}
	}
	if raw, ok := args["remove_saved_queries"]; ok {
		items, ok := raw.([]any)
		if !ok {
			errs = append(errs, "remove_saved_queries must be a list of saved query names")
		}
		for i, item := range items {
			name, _ := item.(string)
			name = strings.TrimSpace(name)
			if !savedQueryNameRe.MatchString(name) {
				errs = append(errs, fmt.Sprintf("remove_saved_queries[%d] %q must use only letters, numbers, underscores and dashes", i, name))
				continue
			}
			removals = append(removals, name)
			changes = append(changes, "removed saved query: "+name)
		}
	}
	return writes, removals, changes, errs
}

func savedQueryPath(name string) string {
	return "/queries/" + name + ".gql"
}

// applySavedQueryUpdates writes and removes saved query files in the config
// folder. A missing file is not an error when it is removed.
func (ms *mcpServer) applySavedQueryUpdates(writes []savedQueryUpdate, removals []string) error {
	if len(writes) == 0 && len(removals) == 0 {
		return nil
	}
	if ms.service == nil || ms.service.fs == nil {
		return errors.New("saved queries need a config folder")
	}
	fs := ms.service.fs
	for _, write := range writes {
		if err := fs.Put(savedQueryPath(write.name), []byte(write.query+"\n")); err != nil {
			return fmt.Errorf("save saved query %s: %w", write.name, err)
		}
	}
	if len(removals) == 0 {
		return nil
	}
	deleter, ok := fs.(interface{ Delete(path string) error })
	if !ok {
		return errors.New("this config folder cannot remove saved queries")
	}
	for _, name := range removals {
		if err := deleter.Delete(savedQueryPath(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove saved query %s: %w", name, err)
		}
	}
	return nil
}

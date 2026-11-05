import React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Pencil, Trash2, Users, User } from "lucide-react";

import { DataErrorState, EmptyState, LoadingState, PageHeader, Panel } from "../components/ui";
import { addSkill, deleteSkill, listSkills, updateSkill } from "../services/agent";
import { operatorIdentityKey, useOperatorIdentity } from "../services/identity";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Textarea } from "@/components/ui/textarea";

const emptyDraft = { name: "", description: "", content: "" };

// Skills lists the instructions the agent follows for recurring work: the
// caller's own skills plus skills teammates shared with the account. The
// agent sees each skill's name and description, and loads the instructions
// when a request matches. Skills are gj_artifacts rows, read and written
// through GraphQL like any other.
const Skills = () => {
  const identityKey = operatorIdentityKey(useOperatorIdentity());
  const queryClient = useQueryClient();
  const skillsQuery = useQuery({ queryKey: ["agent-skills", identityKey], queryFn: listSkills });
  const [draft, setDraft] = React.useState(emptyDraft);
  const [editingID, setEditingID] = React.useState("");
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["agent-skills"] });
  const reset = () => {
    setDraft(emptyDraft);
    setEditingID("");
  };

  const save = useMutation({
    mutationFn: (input) => (editingID ? updateSkill(editingID, input) : addSkill(input)),
    onSuccess: () => {
      reset();
      return refresh();
    },
  });
  const share = useMutation({ mutationFn: ({ id, tier }) => updateSkill(id, { tier }), onSuccess: refresh });
  const remove = useMutation({ mutationFn: deleteSkill, onSuccess: refresh });
  const error = save.error || share.error || remove.error;
  const ready = draft.name.trim() && draft.description.trim() && draft.content.trim();

  function handleSubmit(event) {
    event.preventDefault();
    if (ready) {
      save.mutate({ name: draft.name.trim(), description: draft.description.trim(), content: draft.content.trim() });
    }
  }

  function edit(skill) {
    setEditingID(skill.id);
    setDraft({ name: skill.name || "", description: skill.description || "", content: skill.content || "" });
  }

  const set = (field) => (event) => setDraft((current) => ({ ...current, [field]: event.target.value }));
  const skills = skillsQuery.data || [];
  return (
    <div className="grid gap-6 px-4 py-6 sm:px-6">
      <PageHeader
        title="Skills"
        description="Instructions the agent follows when a request matches."
      />
      <Panel title={editingID ? "Edit" : "Add"}>
        <form className="grid gap-2" onSubmit={handleSubmit}>
          <div className="grid gap-2 sm:grid-cols-2">
            <Input value={draft.name} onChange={set("name")} placeholder="Name" maxLength={64} aria-label="Skill name" />
            <Input
              value={draft.description}
              onChange={set("description")}
              placeholder="When to use it"
              maxLength={200}
              aria-label="Skill description"
            />
          </div>
          <Textarea
            value={draft.content}
            onChange={set("content")}
            placeholder="Instructions"
            rows={4}
            maxLength={8192}
            aria-label="Skill instructions"
          />
          <div className="flex items-center justify-between gap-3">
            {error ? <p className="text-sm text-destructive">{error.message}</p> : <span />}
            <div className="flex items-center gap-2">
              {editingID && (
                <Button type="button" variant="ghost" size="sm" onClick={reset}>
                  Cancel
                </Button>
              )}
              <Button type="submit" size="sm" disabled={!ready || save.isPending}>
                {editingID ? "Save" : "Add"}
              </Button>
            </div>
          </div>
        </form>
      </Panel>
      <Panel title={skills.length ? `Saved · ${skills.length}` : "Saved"}>
        {skillsQuery.isLoading ? (
          <LoadingState />
        ) : skillsQuery.error ? (
          <DataErrorState error={skillsQuery.error} unavailableMessage="Skills could not be read from gj_artifacts." />
        ) : skills.length === 0 ? (
          <EmptyState title="No skills yet" />
        ) : (
          <ul className="grid divide-y">
            {skills.map((skill) => (
              <li key={skill.id} className="flex flex-col gap-2 py-3 sm:flex-row sm:items-start sm:justify-between">
                <div className="grid min-w-0 gap-1">
                  <p className="text-sm font-medium">{skill.name}</p>
                  <p className="text-sm text-muted-foreground">{skill.description}</p>
                  <p className="line-clamp-3 whitespace-pre-wrap break-words text-sm">{skill.content}</p>
                  <div className="flex items-center gap-2">
                    <Badge variant="muted">
                      {skill.scope === "team" ? <Users aria-hidden="true" /> : <User aria-hidden="true" />}
                      {skill.scope === "team" ? "Team" : "Personal"}
                    </Badge>
                    {!skill.mine && <span className="text-xs text-muted-foreground">Shared by a teammate</span>}
                  </div>
                </div>
                {skill.mine && (
                  <div className="flex shrink-0 items-center gap-1">
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      disabled={share.isPending}
                      onClick={() => share.mutate({ id: skill.id, tier: skill.scope === "team" ? "observed" : "approved" })}
                    >
                      {skill.scope === "team" ? "Make personal" : "Share with team"}
                    </Button>
                    <Button type="button" variant="ghost" size="icon" aria-label="Edit skill" onClick={() => edit(skill)}>
                      <Pencil aria-hidden="true" />
                    </Button>
                    <Button type="button" variant="ghost" size="icon" aria-label="Delete skill" disabled={remove.isPending} onClick={() => remove.mutate(skill.id)}>
                      <Trash2 aria-hidden="true" />
                    </Button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </Panel>
    </div>
  );
};

export default Skills;

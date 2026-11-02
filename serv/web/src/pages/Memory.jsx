import React from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Trash2, Users, User } from "lucide-react";

import { DataErrorState, EmptyState, LoadingState, PageHeader, Panel } from "../components/ui";
import { addMemory, deleteMemory, listMemories, updateMemory } from "../services/agent";
import { operatorIdentityKey, useOperatorIdentity } from "../services/identity";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";

// Memory lists the facts the agent recalls in every run: the caller's own
// personal memories plus team memories published to their account.
const Memory = () => {
  const identityKey = operatorIdentityKey(useOperatorIdentity());
  const queryClient = useQueryClient();
  const memoriesQuery = useQuery({ queryKey: ["agent-memories", identityKey], queryFn: listMemories });
  const [draft, setDraft] = React.useState("");
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["agent-memories"] });

  const add = useMutation({ mutationFn: addMemory, onSuccess: () => { setDraft(""); return refresh(); } });
  const update = useMutation({ mutationFn: ({ id, patch }) => updateMemory(id, patch), onSuccess: refresh });
  const remove = useMutation({ mutationFn: deleteMemory, onSuccess: refresh });
  const error = add.error || update.error || remove.error;

  function handleSubmit(event) {
    event.preventDefault();
    if (draft.trim()) {
      add.mutate(draft.trim());
    }
  }

  const memories = memoriesQuery.data || [];
  return (
    <div className="grid gap-6 px-4 py-6 sm:px-6">
      <PageHeader
        eyebrow="User"
        title="Memory"
        description="Facts the agent recalls before every answer. Personal memories stay yours; publish one to share it with your team."
      />
      <Panel title="Add a memory">
        <form className="grid gap-2" onSubmit={handleSubmit}>
          <Textarea
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            placeholder="For example: report revenue in EUR, and our fiscal year starts in April."
            rows={2}
            maxLength={4096}
          />
          <div className="flex items-center justify-between gap-3">
            {error ? <p className="text-sm text-destructive">{error.message}</p> : <span />}
            <Button type="submit" size="sm" disabled={!draft.trim() || add.isPending}>Remember</Button>
          </div>
        </form>
      </Panel>
      <Panel title="Recalled memories" description={`${memories.length} saved`}>
        {memoriesQuery.isLoading ? (
          <LoadingState label="Loading memories" />
        ) : memoriesQuery.error ? (
          <DataErrorState error={memoriesQuery.error} unavailableMessage="The memory endpoint could not be reached at /api/v1/agent/memories." />
        ) : memories.length === 0 ? (
          <EmptyState title="No memories yet" message="Add a fact above, and the agent will use it whenever it is relevant." />
        ) : (
          <ul className="grid divide-y">
            {memories.map((memory) => (
              <li key={memory.id} className="flex flex-col gap-2 py-3 sm:flex-row sm:items-start sm:justify-between">
                <div className="grid min-w-0 gap-1">
                  <p className="whitespace-pre-wrap break-words text-sm">{memory.content}</p>
                  <div className="flex items-center gap-2">
                    <Badge variant="muted">
                      {memory.scope === "team" ? <Users aria-hidden="true" /> : <User aria-hidden="true" />}
                      {memory.scope === "team" ? "Team" : "Personal"}
                    </Badge>
                    {!memory.mine && <span className="text-xs text-muted-foreground">Shared by a teammate</span>}
                  </div>
                </div>
                {memory.mine && (
                  <div className="flex shrink-0 items-center gap-1">
                    <Button
                      type="button"
                      variant="ghost"
                      size="sm"
                      disabled={update.isPending}
                      onClick={() => update.mutate({ id: memory.id, patch: { tier: memory.scope === "team" ? "observed" : "approved" } })}
                    >
                      {memory.scope === "team" ? "Make personal" : "Share with team"}
                    </Button>
                    <Button type="button" variant="ghost" size="icon" aria-label="Delete memory" disabled={remove.isPending} onClick={() => remove.mutate(memory.id)}>
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

export default Memory;

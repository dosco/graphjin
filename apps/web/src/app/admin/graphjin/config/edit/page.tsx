import { connection } from "next/server";
import { readEditableGraphjinConfig } from "@neko/llm/graphjin/config-editor";
import { getCurrentActor } from "@/lib/actor";
import { AdminDenied } from "../../../AdminShell";
import { GraphjinShell, SettingsCard } from "../../GraphjinAdmin";
import ConfigFileEditor from "./ConfigFileEditor";

export default async function AdminGraphjinConfigEditPage() {
  await connection();
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return <AdminDenied />;

  const file = process.env.OPENNEKO_GRAPHJIN_CONFIG?.trim();
  const loaded = file ? await readEditableGraphjinConfig(file).catch((error: unknown) => error) : null;

  return (
    <GraphjinShell
      current="/admin/graphjin/config"
      title="Edit config"
      subtitle="Change GraphJin's config file. Changes are checked first and apply without a restart."
    >
      {!file ? (
        <SettingsCard title="Config file" copy="This deployment has no GraphJin config file to edit." />
      ) : loaded instanceof Error || !loaded ? (
        <SettingsCard
          title="Config file"
          copy={`The config file could not be read: ${loaded instanceof Error ? loaded.message : "unknown error"}`}
        />
      ) : (
        <ConfigFileEditor initial={loaded as Awaited<ReturnType<typeof readEditableGraphjinConfig>>} />
      )}
    </GraphjinShell>
  );
}

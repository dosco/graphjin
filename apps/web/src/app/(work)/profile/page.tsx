import { connection } from "next/server";
import DensityToggle from "@/components/DensityToggle";
import PageHeading from "@/components/PageHeading";
import { getCurrentActor } from "@/lib/actor";
import { getAuthProvider, getCurrentUser } from "@/lib/auth";
import { ProfileClient } from "./ProfileClient";

export default async function ProfilePage() {
  await connection();
  const [user, actor, provider] = await Promise.all([
    getCurrentUser(),
    getCurrentActor(),
    getAuthProvider(),
  ]);
  return (
    <div className="library-page">
      <PageHeading
        title="Your account"
        description={actor.role === "admin" ? "Your persona and sign-in. Administration lives under Admin." : "Your persona and sign-in."}
      />
      <main className="library-main">
        <ProfileClient email={user?.email ?? ""} signInEnabled={Boolean(provider)} />
        <section className="settings-card mt-4 max-w-(--form-max)">
          <div className="settings-card-head">
            <div>
              <h2 className="settings-card-title">Display</h2>
              <p className="settings-card-copy">
                Comfortable shows one column. Compact tiles more on each screen.
              </p>
            </div>
            <DensityToggle />
          </div>
          <p className="text-ui-caption text-text3 tabular-nums">
            OpenNeko {process.env.NEXT_PUBLIC_APP_VERSION ?? "0.0.0"}
          </p>
        </section>
      </main>
    </div>
  );
}

# Context repository

OpenNeko keeps a version history of what your assistant knows and does: skills,
skill learnings, workflows and durable memories. Every
change is saved as a new version, with the date and the person or run that made
it. This is always on. It needs no setup.

Regular users never see versions or git. When a user asks the assistant to change
a skill, the change applies to their own work. An administrator's changes apply to
everyone.

## Optional: connect a git repository

An administrator can connect the company context to a git repository on GitHub,
GitLab or another git host. A remote is optional. Without one, OpenNeko keeps the
full history on your own server.

With a remote you can:

- **Publish** the company skills and workflows to the repository, by a direct push
  or a pull request that someone reviews first.
- **Bring in** skills and packs that someone changed in the repository.

Personal changes are never published.

### Turn it on

1. Create a repository, or choose one where OpenNeko may own the folders you
   publish (for example `skills/` and `workflows/`). OpenNeko leaves every other
   file, such as a README or CI configuration, as it is.
2. Open **Admin → Settings → Context repository**.
3. Enter the repository address and the branch, such as `main`.
4. Give OpenNeko access with an access token over HTTPS, or a deploy key over
   SSH. See [Get access from your git host](#get-access-from-your-git-host).
5. Choose how to publish: **Open a pull request** or **Push to the branch**.
6. Choose what to publish. Skills, skill learnings and workflows are selected by
   default. Memories hold business facts. Publish them only to a private
   repository.
7. Select **Save**, then **Open pull request** or **Push now**.

### Get access from your git host

Use a token for an `https://` address. Use a deploy key for an SSH address, such
as `git@github.com:acme/openneko-context.git`. OpenNeko stores the token and the
private key encrypted.

#### GitHub token (HTTPS)

1. On GitHub, open your profile picture, then **Settings → Developer settings →
   Personal access tokens → Fine-grained tokens**, and select **Generate new
   token**.
2. Enter a name and an expiration date. Set **Resource owner** to the account or
   organization that owns the repository.
3. Under **Repository access**, select **Only select repositories**, then select
   the repository.
4. Under **Permissions → Repository permissions**, set **Contents** to **Read and
   write** and **Pull requests** to **Read and write**.
5. Select **Generate token** and copy it. Paste it into **Access token** in
   OpenNeko. Leave **Username** blank.

An organization can require approval for fine-grained tokens. If OpenNeko cannot
push, ask an organization owner to approve the token. When the token expires,
create a new one and paste it into OpenNeko.

#### GitHub deploy key (SSH)

1. Save the SSH address in OpenNeko, then copy the key that it shows.
2. On GitHub, open the repository, then **Settings → Deploy keys**, and select
   **Add deploy key**.
3. Enter a title, such as `OpenNeko`, paste the key, and select **Allow write
   access**. Select **Add key**.
4. In OpenNeko, compare the fingerprints with
   [GitHub's SSH key fingerprints](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/githubs-ssh-key-fingerprints).
   If they match, select **Confirm host key**.

A deploy key cannot open pull requests. To use pull requests over SSH, also add a
GitHub token in OpenNeko.

#### GitLab token (HTTPS)

1. In GitLab, open the project, then **Settings → Access tokens**, and select
   **Add new token**.
2. Enter a name and an expiration date.
3. Select a role. **Developer** can push branches and open merge requests.
   Select **Maintainer** if OpenNeko pushes directly to a protected branch, such
   as `main`.
4. Select the `api` and `write_repository` scopes.
5. Select **Create project access token** and copy it. Paste it into **Access
   token** in OpenNeko. Leave **Username** blank.

If your GitLab plan has no project access tokens, use a personal access token
from **User settings → Access tokens** with the same scopes.

#### GitLab deploy key (SSH)

1. Save the SSH address in OpenNeko, then copy the key that it shows.
2. In GitLab, open the project, then **Settings → Repository → Deploy keys**, and
   select **Add new key**.
3. Enter a title, paste the key, and select **Grant write permissions to this
   key**. Select **Add key**.
4. To push directly to a protected branch, allow the deploy key under **Settings →
   Repository → Protected branches → Allowed to push and merge**.
5. In OpenNeko, compare the fingerprints with
   [GitLab.com's SSH host key fingerprints](https://docs.gitlab.com/user/gitlab_com/#ssh-host-keys-fingerprints).
   For a self-managed GitLab, ask its administrator for the fingerprints. If they
   match, select **Confirm host key**.

A deploy key cannot open merge requests. To use merge requests over SSH, also add
a GitLab token in OpenNeko.

#### Other git hosts

Use a token or password that can push to the repository. If the host needs the
account name with the token, enter it in **Username**. OpenNeko pushes the
branch, and you open the pull request on the host.

### Bring in changes from the repository

Select **Check for updates** on the same page.

- **Skills.** A skill that changed only in the repository shows **Bring in**. A
  skill that changed in both OpenNeko and the repository asks you to keep one
  version.
- **Packs.** Each `packs/<pack-id>/` folder in the repository is checked with the
  same rules as an uploaded pack. Select **Add** for a new pack or version, then
  install or upgrade it on the Packs page. See [PACKS.md](PACKS.md#release-through-a-context-repository).

Workflows and memories are only published. They do not come back from the
repository.

Every save, publish and update is recorded in the audit log.

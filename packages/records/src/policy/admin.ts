import type { Pool, PoolClient } from "pg";
import { validateRecordIdentifier } from "../naming";

export type RecordAccessSubject = {
  type: "user" | "group";
  /** app_user.id for users; stable sso_group.id for IdP groups. */
  id: string;
};

export type RecordAccessAuditActor = {
  userId: string;
  actionRequestId: string;
};

export class RecordAccessAdminError extends Error {
  readonly code = "records_access_admin_invalid";

  constructor(message: string) {
    super(message);
    this.name = "RecordAccessAdminError";
  }
}

function subject(input: RecordAccessSubject): RecordAccessSubject {
  if (input.type !== "user" && input.type !== "group") {
    throw new RecordAccessAdminError("access subject type must be user or group");
  }
  const id = input.id.trim();
  if (!id || id.length > 500) {
    throw new RecordAccessAdminError("access subject id is required");
  }
  return { type: input.type, id };
}

async function inTransaction<T>(
  pool: Pool,
  operation: (client: PoolClient) => Promise<T>,
): Promise<T> {
  const client = await pool.connect();
  try {
    await client.query("begin");
    const result = await operation(client);
    await client.query("commit");
    return result;
  } catch (error) {
    await client.query("rollback");
    throw error;
  } finally {
    client.release();
  }
}

async function requireApp(client: PoolClient, orgId: string, appId: string): Promise<void> {
  const result = await client.query(
    `select 1 from engine.record_app
     where org_id = $1 and app_id = $2 and status <> 'archived'`,
    [orgId, appId],
  );
  if (!result.rows[0]) throw new RecordAccessAdminError("record app was not found");
}

async function audit(
  client: PoolClient,
  input: {
    orgId: string;
    appId: string;
    subject: RecordAccessSubject;
    resourceKind: "app" | "object" | "field";
    resourceId: string;
    operation: "grant" | "set" | "revoke";
    before: unknown;
    after: unknown;
    actor: RecordAccessAuditActor;
  },
): Promise<void> {
  await client.query(
    `insert into engine.entitlement_audit
       (org_id, app_id, subject_type, subject_id, resource_kind,
        resource_id, operation, before_value, after_value,
        actor_user_id, action_request_id)
     values ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10, $11)`,
    [
      input.orgId,
      input.appId,
      input.subject.type,
      input.subject.id,
      input.resourceKind,
      input.resourceId,
      input.operation,
      input.before === null ? null : JSON.stringify(input.before),
      input.after === null ? null : JSON.stringify(input.after),
      input.actor.userId,
      input.actor.actionRequestId,
    ],
  );
}

export class RecordsAccessAdmin {
  constructor(private readonly pool: Pool) {}

  async grantApp(input: {
    orgId: string;
    appId: string;
    subject: RecordAccessSubject;
    actor: RecordAccessAuditActor;
  }): Promise<{ granted: true }> {
    const appId = validateRecordIdentifier(input.appId);
    const target = subject(input.subject);
    return inTransaction(this.pool, async (client) => {
      await requireApp(client, input.orgId, appId);
      await client.query(
        `insert into engine.app_access_grant
           (org_id, app_id, subject_type, subject_id,
            created_by_user_id, action_request_id)
         values ($1, $2, $3, $4, $5, $6)
         on conflict (org_id, app_id, subject_type, subject_id) do nothing`,
        [
          input.orgId,
          appId,
          target.type,
          target.id,
          input.actor.userId,
          input.actor.actionRequestId,
        ],
      );
      await audit(client, {
        ...input,
        appId,
        subject: target,
        resourceKind: "app",
        resourceId: appId,
        operation: "grant",
        before: null,
        after: { granted: true },
      });
      return { granted: true };
    });
  }

  async revokeApp(input: {
    orgId: string;
    appId: string;
    subject: RecordAccessSubject;
    actor: RecordAccessAuditActor;
  }): Promise<{ revoked: boolean }> {
    const appId = validateRecordIdentifier(input.appId);
    const target = subject(input.subject);
    return inTransaction(this.pool, async (client) => {
      await requireApp(client, input.orgId, appId);
      const childObjects = await client.query<{
        object_api_name: string;
        can_read: boolean;
        can_create: boolean;
        can_update: boolean;
        can_delete: boolean;
      }>(
        `select object_api_name, can_read, can_create, can_update, can_delete
         from engine.object_access_grant
         where org_id = $1 and app_id = $2
           and subject_type = $3 and subject_id = $4
         order by object_api_name`,
        [input.orgId, appId, target.type, target.id],
      );
      const childFields = await client.query<{
        object_api_name: string;
        field_api_name: string;
        can_read: boolean;
        can_write: boolean;
      }>(
        `select g.object_api_name, f.api_name as field_api_name,
                g.can_read, g.can_write
         from engine.field_access_grant g
         join engine.record_field f on f.id = g.field_id and f.org_id = g.org_id
         where g.org_id = $1 and g.app_id = $2
           and g.subject_type = $3 and g.subject_id = $4
         order by g.object_api_name, f.api_name`,
        [input.orgId, appId, target.type, target.id],
      );
      const deleted = await client.query(
        `delete from engine.app_access_grant
         where org_id = $1 and app_id = $2
           and subject_type = $3 and subject_id = $4
         returning 1`,
        [input.orgId, appId, target.type, target.id],
      );
      const revoked = deleted.rowCount === 1;
      if (revoked) {
        for (const field of childFields.rows) {
          await audit(client, {
            ...input,
            appId,
            subject: target,
            resourceKind: "field",
            resourceId: `${field.object_api_name}.${field.field_api_name}`,
            operation: "revoke",
            before: { canRead: field.can_read, canWrite: field.can_write },
            after: null,
          });
        }
        for (const object of childObjects.rows) {
          await audit(client, {
            ...input,
            appId,
            subject: target,
            resourceKind: "object",
            resourceId: object.object_api_name,
            operation: "revoke",
            before: {
              canRead: object.can_read,
              canCreate: object.can_create,
              canUpdate: object.can_update,
              canDelete: object.can_delete,
            },
            after: null,
          });
        }
      }
      await audit(client, {
        ...input,
        appId,
        subject: target,
        resourceKind: "app",
        resourceId: appId,
        operation: "revoke",
        before: revoked ? { granted: true } : null,
        after: null,
      });
      return { revoked };
    });
  }

  async setObject(input: {
    orgId: string;
    appId: string;
    objectApiName: string;
    subject: RecordAccessSubject;
    permissions: {
      canRead: boolean;
      canCreate: boolean;
      canUpdate: boolean;
      canDelete: boolean;
    };
    actor: RecordAccessAuditActor;
  }): Promise<{ updated: true }> {
    const appId = validateRecordIdentifier(input.appId);
    const objectApiName = validateRecordIdentifier(input.objectApiName);
    const target = subject(input.subject);
    const permissions = input.permissions;
    if (
      !permissions.canRead &&
      (permissions.canCreate || permissions.canUpdate || permissions.canDelete)
    ) {
      throw new RecordAccessAdminError("object write permissions require read access");
    }
    return inTransaction(this.pool, async (client) => {
      await requireApp(client, input.orgId, appId);
      const parent = await client.query(
        `select 1 from engine.app_access_grant
         where org_id = $1 and app_id = $2
           and subject_type = $3 and subject_id = $4`,
        [input.orgId, appId, target.type, target.id],
      );
      if (!parent.rows[0]) {
        throw new RecordAccessAdminError("grant app access before object access");
      }
      const object = await client.query(
        `select 1 from engine.record_object
         where org_id = $1 and app_id = $2 and api_name = $3
           and archived_at is null`,
        [input.orgId, appId, objectApiName],
      );
      if (!object.rows[0]) throw new RecordAccessAdminError("record object was not found");
      const before = await client.query(
        `select can_read as "canRead", can_create as "canCreate",
                can_update as "canUpdate", can_delete as "canDelete"
         from engine.object_access_grant
         where org_id = $1 and app_id = $2 and subject_type = $3
           and subject_id = $4 and object_api_name = $5`,
        [input.orgId, appId, target.type, target.id, objectApiName],
      );
      await client.query(
        `insert into engine.object_access_grant
           (org_id, app_id, subject_type, subject_id, object_api_name,
            can_read, can_create, can_update, can_delete,
            created_by_user_id, action_request_id)
         values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
         on conflict (org_id, app_id, subject_type, subject_id, object_api_name)
         do update set
           can_read = excluded.can_read,
           can_create = excluded.can_create,
           can_update = excluded.can_update,
           can_delete = excluded.can_delete,
           updated_at = now(),
           action_request_id = excluded.action_request_id`,
        [
          input.orgId,
          appId,
          target.type,
          target.id,
          objectApiName,
          permissions.canRead,
          permissions.canCreate,
          permissions.canUpdate,
          permissions.canDelete,
          input.actor.userId,
          input.actor.actionRequestId,
        ],
      );
      await audit(client, {
        ...input,
        appId,
        subject: target,
        resourceKind: "object",
        resourceId: objectApiName,
        operation: "set",
        before: before.rows[0] ?? null,
        after: permissions,
      });
      return { updated: true };
    });
  }

  /**
   * A field is hidden until a subject holds a grant for it. Give every user
   * and group that can read the object the same access to a new field:
   * read, plus write when it can create or update records.
   */
  async grantFieldLikeObject(input: {
    orgId: string;
    appId: string;
    objectApiName: string;
    fieldApiName: string;
    actor: RecordAccessAuditActor;
  }): Promise<{ granted: number }> {
    const grants = await this.pool.query<{
      subject_type: "user" | "group";
      subject_id: string;
      can_write: boolean;
    }>(
      `select g.subject_type, g.subject_id, (g.can_create or g.can_update) and not f.read_only as can_write
       from engine.object_access_grant g
       join engine.record_object o
         on o.org_id = g.org_id and o.app_id = g.app_id and o.api_name = g.object_api_name
       join engine.record_field f on f.object_id = o.id and f.org_id = o.org_id
       where g.org_id = $1 and g.app_id = $2 and g.object_api_name = $3 and g.can_read
         and g.subject_type in ('user', 'group')
         and f.api_name = $4 and f.archived_at is null`,
      [input.orgId, input.appId, input.objectApiName, input.fieldApiName],
    );
    for (const grant of grants.rows) {
      await this.setField({
        orgId: input.orgId,
        appId: input.appId,
        objectApiName: input.objectApiName,
        fieldApiName: input.fieldApiName,
        subject: { type: grant.subject_type, id: grant.subject_id },
        permissions: { canRead: true, canWrite: grant.can_write },
        actor: input.actor,
      });
    }
    return { granted: grants.rows.length };
  }

  async setField(input: {
    orgId: string;
    appId: string;
    objectApiName: string;
    fieldApiName: string;
    subject: RecordAccessSubject;
    permissions: { canRead: boolean; canWrite: boolean };
    actor: RecordAccessAuditActor;
  }): Promise<{ updated: true }> {
    const appId = validateRecordIdentifier(input.appId);
    const objectApiName = validateRecordIdentifier(input.objectApiName);
    const fieldApiName = validateRecordIdentifier(input.fieldApiName);
    const target = subject(input.subject);
    if (input.permissions.canWrite && !input.permissions.canRead) {
      throw new RecordAccessAdminError("field write permission requires read access");
    }
    return inTransaction(this.pool, async (client) => {
      await requireApp(client, input.orgId, appId);
      const objectGrant = await client.query<{
        can_create: boolean;
        can_update: boolean;
      }>(
        `select can_create, can_update from engine.object_access_grant
         where org_id = $1 and app_id = $2 and subject_type = $3
           and subject_id = $4 and object_api_name = $5`,
        [input.orgId, appId, target.type, target.id, objectApiName],
      );
      const parent = objectGrant.rows[0];
      if (!parent) {
        throw new RecordAccessAdminError("set object access before field access");
      }
      if (input.permissions.canWrite && !parent.can_create && !parent.can_update) {
        throw new RecordAccessAdminError("field write requires parent object create or update");
      }
      const field = await client.query<{ id: string; read_only: boolean }>(
        `select f.id::text as id, f.read_only
         from engine.record_field f
         join engine.record_object o on o.id = f.object_id and o.org_id = f.org_id
         where o.org_id = $1 and o.app_id = $2 and o.api_name = $3
           and f.api_name = $4 and o.archived_at is null and f.archived_at is null`,
        [input.orgId, appId, objectApiName, fieldApiName],
      );
      const fieldRow = field.rows[0];
      if (!fieldRow) throw new RecordAccessAdminError("record field was not found");
      if (input.permissions.canWrite && fieldRow.read_only) {
        throw new RecordAccessAdminError("read-only schema fields cannot receive write access");
      }
      const before = await client.query(
        `select can_read as "canRead", can_write as "canWrite"
         from engine.field_access_grant
         where org_id = $1 and app_id = $2 and subject_type = $3
           and subject_id = $4 and field_id = $5::uuid`,
        [input.orgId, appId, target.type, target.id, fieldRow.id],
      );
      await client.query(
        `insert into engine.field_access_grant
           (org_id, app_id, subject_type, subject_id, object_api_name,
            field_id, can_read, can_write, created_by_user_id,
            action_request_id)
         values ($1, $2, $3, $4, $5, $6::uuid, $7, $8, $9, $10)
         on conflict (org_id, app_id, subject_type, subject_id, field_id)
         do update set
           can_read = excluded.can_read,
           can_write = excluded.can_write,
           updated_at = now(),
           action_request_id = excluded.action_request_id`,
        [
          input.orgId,
          appId,
          target.type,
          target.id,
          objectApiName,
          fieldRow.id,
          input.permissions.canRead,
          input.permissions.canWrite,
          input.actor.userId,
          input.actor.actionRequestId,
        ],
      );
      await audit(client, {
        ...input,
        appId,
        subject: target,
        resourceKind: "field",
        resourceId: `${objectApiName}.${fieldApiName}`,
        operation: "set",
        before: before.rows[0] ?? null,
        after: input.permissions,
      });
      return { updated: true };
    });
  }
}

import { zValidator } from "@hono/zod-validator";
import { asc, eq, gt } from "drizzle-orm";
import { Hono } from "hono";
import z from "zod";
import { db } from "../db";
import * as schema from "../db-schema";

const requestSchema = z.discriminatedUnion("type", [
  z.object({
    type: z.literal("aws"),
    name: z.string(),
    role_arn: z.string(),
    kms_arn: z.string(),
    external_id: z.string(),
  }),
  z.object({
    type: z.literal("azure"),
    name: z.string(),
    tenant_id: z.string(),
    vault_uri: z.string(),
    key_name: z.string(),
  }),
  z.object({
    type: z.literal("gcp"),
    name: z.string(),
    workload_identity_project_number: z.string(),
    workload_identity_pool_id: z.string(),
    workload_identity_provider_id: z.string(),
    audience: z.string(),
    kms_project_id: z.string(),
    kms_key_ring_name: z.string(),
    kms_key_name: z.string(),
    kms_key_location: z.string(),
  }),
]);

function serialize(key: typeof schema.externalKeys.$inferSelect) {
  const { config, ...rest } = key;
  return { ...rest, ...config, api_project_ids: [] };
}

const route = new Hono();

route.get("/", async (c) => {
  const limit = Number(c.req.query("limit") ?? 20);
  const after = c.req.query("after");

  const keys = await db.query.externalKeys.findMany({
    where: after ? gt(schema.externalKeys.id, after) : undefined,
    orderBy: asc(schema.externalKeys.id),
    limit: limit + 1,
  });
  const data = keys.slice(0, limit).map(serialize);

  return c.json({
    object: "list",
    data,
    has_more: keys.length > limit,
    first_id: data.at(0)?.id,
    last_id: data.at(-1)?.id,
  });
});

route.post("/", zValidator("json", requestSchema), async (c) => {
  const { type, name, ...config } = c.req.valid("json");

  const [key] = await db
    .insert(schema.externalKeys)
    .values({ type, name, config })
    .returning();
  if (!key) {
    return c.json({ error: "Failed to create external key" }, 500);
  }

  return c.json(serialize(key));
});

route.delete("/:key_id", async (c) => {
  const key_id = c.req.param("key_id");

  const [key] = await db
    .delete(schema.externalKeys)
    .where(eq(schema.externalKeys.id, key_id))
    .returning();
  if (!key) {
    return c.json({ error: "External key not found" }, 404);
  }

  return c.json({
    object: "organization.external_key.deleted",
    id: key.id,
    deleted: true,
  });
});

export default route;

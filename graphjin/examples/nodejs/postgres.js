import graphjin from "graphjin";
import express from "express";
import http from "http";

import PgPool from "pg-pool";

const db = new PgPool({
  host: "localhost",
  port: 5432,
  user: "postgres",
  password: "postgres",
  database: "42papers-development",
});

// config can either be a filename (eg. `dev.yml`) or an object
// const config = { production: true, default_limit: 50 };

const app = express();
const server = http.createServer(app);
const gj = await graphjin("./config", "dev.yml", db);

const res1 = await gj.subscribe(
  "subscription getUpdatedUser { users(id: $userID) { id email } }",
  null,
  { userID: 2 }
);

res1.data(function (res) {
  console.log(">", res.data());
});

app.get("/", async function (req, resp) {
  const res2 = await gj.query(
    "query getUser { users(id: $id) { id email } }",
    { id: 1 },
    { userID: 1 }
  );

  resp.send(res2.data());
});

server.listen(3000);
console.log("Express server started on port %s", server.address().port);

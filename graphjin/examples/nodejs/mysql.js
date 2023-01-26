import graphjin from "graphjin";
import express from "express";
import http from "http";
import mysql from "mysql2";

const pool = mysql.createPool({
  host: "localhost",
  port: "/tmp/mysql.sock",
  user: "root",
  database: "db",
  waitForConnections: true,
  connectionLimit: 10,
  queueLimit: 0,
});

const db = pool.promise();
const app = express();
const server = http.createServer(app);

// config can either be a filename (eg. `dev.yml`) or an object
const config = {
  production: false,
  db_type: "mysql",
  disable_allow_list: true,
};

const gj = await graphjin("./config", config, db);

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

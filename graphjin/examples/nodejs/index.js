import graphjin from "graphjin";
// import express from "express";
// import http from "http";
import pg from "pg"

const { Client } = pg
const db = new Client({
    host: 'localhost',
    port: 5432,
    user: 'postgres',
    password: 'postgres',
    database: "42papers-development"
})

await db.connect()

// config can either be a file (eg. `dev.yml`) or an object
// const config = { production: true, default_limit: 50 };

// var app = express();
// var server = http.createServer(app);

// const res1 = await gj.subscribe(
//     "subscription getUpdatedUser { users(id: $userID) { id email } }", 
//     null,
//     { userID: 2 })

// res1.data(function(res) {
//     console.log(">", res.data())
// })

var gj = await graphjin("./config", "dev.yml", db);


const q = `
query test { 
    organization_users (where: { organization_id: $id} ) {
      role
      users {
        id
        email
      }
    }
  }
`

for (let i = 0; i < 10000;i++) {
    const res = await gj.query(q, { id: 1 })
    console.log(i, JSON.stringify(res.data()))
// console.log(res.sql())
}
process.exit(0);

// app.get('/', async function(req, resp) {
   

//     resp.send(res2.data());
// });

// server.listen(3000);
// console.log('Express server started on port %s', server.address().port);
import ws from 'k6/ws'
import crypto from 'k6/crypto';
import { check } from 'k6'

const URL = 'ws://localhost:8080/api/v1/graphql';
const HEADERS = [ { "X-User-ID": 3 }];

const QUERY = `
subscription {
  products(
    first: 2,
    after: $cursor) {
    id
    NAME
    user {
      full_name
      picture : avatar
      email
      category_counts(limit: 2) {
        count
        category {
          name
        }
      }
    }
    category(limit: 2) {
      id
      name
    }
  }
}`

const VARIABLES = { cursor: null };

export default function () {
  let url = URL
  if (__ENV.url) { url = __ENV.url }

  let body = JSON.stringify({ 
    id: crypto.md5(crypto.randomBytes(42), "hex"),
    type: "subscribe",
    payload: { query: QUERY, variables: VARIABLES },
  })

  let count = 0

  // Send the request
  const res = ws.connect(url, null, function (socket) {
    socket.on('open', () => {
      console.log('connected')
      socket.send(`{"type": "connection_init"}`);
    });

    socket.on('message', (data) => {
      console.log('Message received: ', data)
      let d = JSON.parse(data)
      
      if (d.type === "connection_ack") {
        socket.send(body);
      }

      if (d.type === "next") {
        count += 2
        if (count > 20) {
          socket.close()
        }
      }
    });

    socket.on('close', () => console.log('disconnected'));
  });

  // Run assertions on status, errors in body, optionally results count
  check(res, {
    'is status 101': (r) => r.status === 101,
    'no error in body': (r) => Boolean(r.json('errors')) == false,
  })
}

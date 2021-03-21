import http from 'k6/http'
import { check } from 'k6'

const URL = 'http://localhost:8080/api/v1/graphql';
const HEADERS = [ { "X-User-ID": 3 }];

const QUERY = `
query {
  products(
    limit: 1,
    order_by: { price: desc },
    where: { id: { and: { greater_or_equals: 20, lt: 28 } } }) {
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

const VARIABLES = {};

export default function () {
  let url = URL
  if (__ENV.url) { url = __ENV.url }

  // Prepare query & variables (if provided)
  let body = JSON.stringify({ query: QUERY, variables: VARIABLES })

  // Send the request
  let res = http.post(url, body, HEADERS)

  // Run assertions on status, errors in body, optionally results count
  check(res, {
    'is status 200': (r) => r.status === 200,
    'no error in body': (r) => Boolean(r.json('errors')) == false,
  })
}

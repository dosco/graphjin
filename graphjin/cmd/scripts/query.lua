wrk.method = "POST"
wrk.headers["Content-Type"] = "application/json"
wrk.body = [[{"query":" { products( limit: 30, order_by: { price: desc }, distinct: [ price ] where: { id: { and: { greater_or_equals: 20, lt: 28 } } }) { id name price user { id email } } } "}]]

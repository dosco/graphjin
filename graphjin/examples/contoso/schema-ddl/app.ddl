# GraphJin DDL for the Contoso Retail reference environment (SQLite, in-process).
# Applied by `graphjin serve --demo --path examples/contoso`.

type customer {
  id: Bigint! @id
  customer_key: Bigint! @unique
  name: Text
  gender: Text
  city: Text
  state: Text
  state_code: Text
  zip_code: Text
  country: Text
  country_code: Text
  continent: Text
  birthday: Date!
  age: Integer!
}

type product {
  id: Bigint! @id
  product_key: Bigint! @unique
  product_code: Text!
  product_name: Text
  manufacturer: Text
  brand: Text
  color: Text
  unit_cost: Numeric
  unit_price: Numeric
  category_code: Integer
  category: Text
  subcategory_code: Integer
  subcategory: Text
}

type store {
  id: Bigint! @id
  store_key: Bigint! @unique
  store_code: Integer!
  name: Text
  country: Text
  state: Text
  square_meters: Integer
  open_date: Date
  close_date: Date
  status: Text
}

type currency_exchange {
  id: Bigint! @id
  date: Date!
  from_currency: Text!
  to_currency: Text!
  exchange: Numeric!
}

type date {
  id: Bigint! @id
  date: Date!
  year: Integer!
  month: Text!
  month_number: Integer!
  quarter: Text!
  working_day: Boolean!
}

type orders {
  id: Bigint! @id
  order_key: Bigint! @unique
  customer_key: Bigint! @relation(type: customer, field: customer_key)
  store_key: Bigint! @relation(type: store, field: store_key)
  order_date: Date!
  delivery_date: Date!
  currency_code: Text!
}

type order_rows {
  id: Bigint! @id
  order_key: Bigint! @relation(type: orders, field: order_key)
  line_number: Integer!
  product_key: Bigint! @relation(type: product, field: product_key)
  quantity: Integer!
  unit_price: Numeric!
  net_price: Numeric!
  unit_cost: Numeric!
}

type sales {
  id: Bigint! @id
  order_number: Bigint!
  line_number: Integer!
  order_date: Date!
  delivery_date: Date!
  customer_key: Bigint! @relation(type: customer, field: customer_key)
  store_key: Bigint! @relation(type: store, field: store_key)
  product_key: Bigint! @relation(type: product, field: product_key)
  quantity: Integer!
  unit_price: Numeric!
  net_price: Numeric!
  unit_cost: Numeric!
  currency_code: Text!
  exchange_rate: Numeric!
}

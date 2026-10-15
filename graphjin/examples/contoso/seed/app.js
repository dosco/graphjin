// Seed data for the Contoso Retail reference environment (SQLite, in-process).
// Applied on demo boot by `graphjin serve --demo --path examples/contoso`.

const seedOptions = { source: "app", user_id: "demo-seed", role: "user" };

function insert(query, variables) {
  return graphql(query, variables, seedOptions);
}

const DAY_MS = 86400000;
// Fixed reference epoch for deterministic eval repeatability
const baseEpochMs = new Date("2026-06-01T00:00:00Z").getTime();

function refDay(offsetDays) {
  return new Date(baseEpochMs + offsetDays * DAY_MS).toISOString().slice(0, 10);
}

// 1. Customers
insert(
  `mutation {
    customer(insert: $customers) { id }
  }`,
  {
    customers: [
      {
        id: 1, customer_key: 101, name: "Aaron Roberts", gender: "M",
        city: "Seattle", state: "Washington", state_code: "WA", zip_code: "98101",
        country: "United States", country_code: "US", continent: "North America",
        birthday: "1988-04-12", age: 38
      },
      {
        id: 2, customer_key: 102, name: "Elena Rostova", gender: "F",
        city: "San Francisco", state: "California", state_code: "CA", zip_code: "94105",
        country: "United States", country_code: "US", continent: "North America",
        birthday: "1994-08-23", age: 32
      },
      {
        id: 3, customer_key: 103, name: "Marcus Chen", gender: "M",
        city: "Austin", state: "Texas", state_code: "TX", zip_code: "78701",
        country: "United States", country_code: "US", continent: "North America",
        birthday: "1982-11-05", age: 44
      },
      {
        id: 4, customer_key: 104, name: "Sophia Miller", gender: "F",
        city: "New York", state: "New York", state_code: "NY", zip_code: "10001",
        country: "United States", country_code: "US", continent: "North America",
        birthday: "1999-01-19", age: 27
      },
      {
        id: 5, customer_key: 105, name: "Liam O'Connor", gender: "M",
        city: "Chicago", state: "Illinois", state_code: "IL", zip_code: "60601",
        country: "United States", country_code: "US", continent: "North America",
        birthday: "1991-06-30", age: 35
      },
      {
        id: 6, customer_key: 106, name: "Chloe Dupont", gender: "F",
        city: "Bellevue", state: "Washington", state_code: "WA", zip_code: "98004",
        country: "United States", country_code: "US", continent: "North America",
        birthday: "2001-09-14", age: 25
      },
      {
        id: 7, customer_key: 107, name: "Devon Vance", gender: "M",
        city: "Los Angeles", state: "California", state_code: "CA", zip_code: "90012",
        country: "United States", country_code: "US", continent: "North America",
        birthday: "1985-03-27", age: 41
      },
      {
        id: 8, customer_key: 108, name: "Maya Patel", gender: "F",
        city: "Dallas", state: "Texas", state_code: "TX", zip_code: "75201",
        country: "United States", country_code: "US", continent: "North America",
        birthday: "1996-12-08", age: 30
      }
    ]
  }
);

// 2. Products
insert(
  `mutation {
    product(insert: $products) { id }
  }`,
  {
    products: [
      {
        id: 1, product_key: 201, product_code: "PROD-AUD-01", product_name: "Contoso Wireless Noise-Cancelling Headphones",
        manufacturer: "Contoso Ltd", brand: "Contoso", color: "Black",
        unit_cost: 110.00, unit_price: 249.99, category_code: 1, category: "Audio",
        subcategory_code: 101, subcategory: "Headphones"
      },
      {
        id: 2, product_key: 202, product_code: "PROD-AUD-02", product_name: "Fabrikam Studio Earbuds Pro",
        manufacturer: "Fabrikam Inc", brand: "Fabrikam", color: "White",
        unit_cost: 65.00, unit_price: 149.50, category_code: 1, category: "Audio",
        subcategory_code: 102, subcategory: "Earbuds"
      },
      {
        id: 3, product_key: 203, product_code: "PROD-CMP-01", product_name: "Litware Ultralight 14-inch Laptop",
        manufacturer: "Litware Corp", brand: "Litware", color: "Silver",
        unit_cost: 540.00, unit_price: 1099.00, category_code: 2, category: "Computers",
        subcategory_code: 201, subcategory: "Laptops"
      },
      {
        id: 4, product_key: 204, product_code: "PROD-CMP-02", product_name: "Contoso Ergonomic Mechanical Keyboard",
        manufacturer: "Contoso Ltd", brand: "Contoso", color: "Black",
        unit_cost: 45.00, unit_price: 119.99, category_code: 2, category: "Computers",
        subcategory_code: 202, subcategory: "Accessories"
      },
      {
        id: 5, product_key: 205, product_code: "PROD-CAM-01", product_name: "Adventure Works 4K Action Camera",
        manufacturer: "Adventure Works", brand: "Adventure Works", color: "Blue",
        unit_cost: 180.00, unit_price: 399.00, category_code: 3, category: "Cameras",
        subcategory_code: 301, subcategory: "Action Cams"
      },
      {
        id: 6, product_key: 206, product_code: "PROD-TV-01", product_name: "Fabrikam 55-inch OLED Smart 4K TV",
        manufacturer: "Fabrikam Inc", brand: "Fabrikam", color: "Black",
        unit_cost: 650.00, unit_price: 1299.99, category_code: 4, category: "TV and Video",
        subcategory_code: 401, subcategory: "Televisions"
      },
      {
        id: 7, product_key: 207, product_code: "PROD-AUD-03", product_name: "Litware Bluetooth Portable Speaker",
        manufacturer: "Litware Corp", brand: "Litware", color: "Blue",
        unit_cost: 32.00, unit_price: 79.99, category_code: 1, category: "Audio",
        subcategory_code: 103, subcategory: "Speakers"
      },
      {
        id: 8, product_key: 208, product_code: "PROD-CMP-03", product_name: "Adventure Works Precision Gaming Mouse",
        manufacturer: "Adventure Works", brand: "Adventure Works", color: "Black",
        unit_cost: 28.00, unit_price: 69.95, category_code: 2, category: "Computers",
        subcategory_code: 202, subcategory: "Accessories"
      }
    ]
  }
);

// 3. Stores
insert(
  `mutation {
    store(insert: $stores) { id }
  }`,
  {
    stores: [
      {
        id: 1, store_key: 301, store_code: 1001, name: "Seattle Flagship Store",
        country: "United States", state: "Washington", square_meters: 3500,
        open_date: "2015-05-10", close_date: null, status: "Open"
      },
      {
        id: 2, store_key: 302, store_code: 1002, name: "San Francisco Tech Center",
        country: "United States", state: "California", square_meters: 2800,
        open_date: "2017-09-15", close_date: null, status: "Open"
      },
      {
        id: 3, store_key: 303, store_code: 1003, name: "Austin Galleria",
        country: "United States", state: "Texas", square_meters: 2200,
        open_date: "2019-03-20", close_date: null, status: "Open"
      },
      {
        id: 4, store_key: 304, store_code: 1004, name: "New York 5th Avenue",
        country: "United States", state: "New York", square_meters: 4200,
        open_date: "2014-11-01", close_date: null, status: "Open"
      },
      {
        id: 5, store_key: 305, store_code: 1005, name: "Chicago Magnificent Mile",
        country: "United States", state: "Illinois", square_meters: 3100,
        open_date: "2018-06-12", close_date: null, status: "Open"
      },
      {
        id: 6, store_key: 306, store_code: 1006, name: "Dallas Galleria (Renovated)",
        country: "United States", state: "Texas", square_meters: 1900,
        open_date: "2016-01-18", close_date: "2025-12-31", status: "Closed"
      }
    ]
  }
);

// 4. Currency Exchange
insert(
  `mutation {
    currency_exchange(insert: $rates) { id }
  }`,
  {
    rates: [
      { id: 1, date: refDay(-60), from_currency: "USD", to_currency: "EUR", exchange: 0.92 },
      { id: 2, date: refDay(-45), from_currency: "USD", to_currency: "EUR", exchange: 0.915 },
      { id: 3, date: refDay(-30), from_currency: "USD", to_currency: "EUR", exchange: 0.922 },
      { id: 4, date: refDay(-15), from_currency: "USD", to_currency: "EUR", exchange: 0.918 },
      { id: 5, date: refDay(0),   from_currency: "USD", to_currency: "EUR", exchange: 0.925 },
      { id: 6, date: refDay(-60), from_currency: "USD", to_currency: "CAD", exchange: 1.35 },
      { id: 7, date: refDay(-30), from_currency: "USD", to_currency: "CAD", exchange: 1.355 },
      { id: 8, date: refDay(0),   from_currency: "USD", to_currency: "CAD", exchange: 1.36 }
    ]
  }
);

// 5. Date Table
insert(
  `mutation {
    date(insert: $dates) { id }
  }`,
  {
    dates: [
      { id: 1, date: refDay(-60), year: 2026, month: "April", month_number: 4, quarter: "Q2", working_day: true },
      { id: 2, date: refDay(-45), year: 2026, month: "April", month_number: 4, quarter: "Q2", working_day: true },
      { id: 3, date: refDay(-30), year: 2026, month: "May",   month_number: 5, quarter: "Q2", working_day: true },
      { id: 4, date: refDay(-15), year: 2026, month: "May",   month_number: 5, quarter: "Q2", working_day: true },
      { id: 5, date: refDay(-7),  year: 2026, month: "May",   month_number: 5, quarter: "Q2", working_day: true },
      { id: 6, date: refDay(-3),  year: 2026, month: "May",   month_number: 5, quarter: "Q2", working_day: false },
      { id: 7, date: refDay(0),   year: 2026, month: "June",  month_number: 6, quarter: "Q2", working_day: true },
      { id: 8, date: refDay(2),   year: 2026, month: "June",  month_number: 6, quarter: "Q2", working_day: true }
    ]
  }
);

// 6. Orders
insert(
  `mutation {
    orders(insert: $orders) { id }
  }`,
  {
    orders: [
      { id: 1, order_key: 5001, customer_key: 101, store_key: 301, order_date: refDay(-45), delivery_date: refDay(-41), currency_code: "USD" },
      { id: 2, order_key: 5002, customer_key: 102, store_key: 302, order_date: refDay(-35), delivery_date: refDay(-31), currency_code: "USD" },
      { id: 3, order_key: 5003, customer_key: 103, store_key: 303, order_date: refDay(-25), delivery_date: refDay(-20), currency_code: "USD" },
      { id: 4, order_key: 5004, customer_key: 104, store_key: 304, order_date: refDay(-20), delivery_date: refDay(-16), currency_code: "USD" },
      { id: 5, order_key: 5005, customer_key: 105, store_key: 305, order_date: refDay(-14), delivery_date: refDay(-10), currency_code: "USD" },
      { id: 6, order_key: 5006, customer_key: 106, store_key: 301, order_date: refDay(-10), delivery_date: refDay(-6),  currency_code: "USD" },
      { id: 7, order_key: 5007, customer_key: 107, store_key: 302, order_date: refDay(-5),  delivery_date: refDay(-1),  currency_code: "USD" },
      { id: 8, order_key: 5008, customer_key: 108, store_key: 303, order_date: refDay(-2),  delivery_date: refDay(2),   currency_code: "USD" }
    ]
  }
);

// 7. Order Rows
insert(
  `mutation {
    order_rows(insert: $rows) { id }
  }`,
  {
    rows: [
      { id: 1, order_key: 5001, line_number: 1, product_key: 201, quantity: 1, unit_price: 249.99, net_price: 249.99, unit_cost: 110.00 },
      { id: 2, order_key: 5001, line_number: 2, product_key: 204, quantity: 2, unit_price: 119.99, net_price: 239.98, unit_cost: 45.00 },
      { id: 3, order_key: 5002, line_number: 1, product_key: 203, quantity: 1, unit_price: 1099.00, net_price: 1099.00, unit_cost: 540.00 },
      { id: 4, order_key: 5003, line_number: 1, product_key: 202, quantity: 2, unit_price: 149.50, net_price: 299.00, unit_cost: 65.00 },
      { id: 5, order_key: 5003, line_number: 2, product_key: 207, quantity: 1, unit_price: 79.99, net_price: 79.99, unit_cost: 32.00 },
      { id: 6, order_key: 5004, line_number: 1, product_key: 206, quantity: 1, unit_price: 1299.99, net_price: 1299.99, unit_cost: 650.00 },
      { id: 7, order_key: 5005, line_number: 1, product_key: 205, quantity: 1, unit_price: 399.00, net_price: 399.00, unit_cost: 180.00 },
      { id: 8, order_key: 5006, line_number: 1, product_key: 201, quantity: 2, unit_price: 249.99, net_price: 499.98, unit_cost: 110.00 },
      { id: 9, order_key: 5007, line_number: 1, product_key: 208, quantity: 3, unit_price: 69.95, net_price: 209.85, unit_cost: 28.00 },
      { id: 10, order_key: 5008, line_number: 1, product_key: 204, quantity: 1, unit_price: 119.99, net_price: 119.99, unit_cost: 45.00 }
    ]
  }
);

// 8. Sales (Transactions)
insert(
  `mutation {
    sales(insert: $sales) { id }
  }`,
  {
    sales: [
      { id: 1, order_number: 5001, line_number: 1, order_date: refDay(-45), delivery_date: refDay(-41), customer_key: 101, store_key: 301, product_key: 201, quantity: 1, unit_price: 249.99, net_price: 249.99, unit_cost: 110.00, currency_code: "USD", exchange_rate: 1.0 },
      { id: 2, order_number: 5001, line_number: 2, order_date: refDay(-45), delivery_date: refDay(-41), customer_key: 101, store_key: 301, product_key: 204, quantity: 2, unit_price: 119.99, net_price: 239.98, unit_cost: 45.00, currency_code: "USD", exchange_rate: 1.0 },
      { id: 3, order_number: 5002, line_number: 1, order_date: refDay(-35), delivery_date: refDay(-31), customer_key: 102, store_key: 302, product_key: 203, quantity: 1, unit_price: 1099.00, net_price: 1099.00, unit_cost: 540.00, currency_code: "USD", exchange_rate: 1.0 },
      { id: 4, order_number: 5003, line_number: 1, order_date: refDay(-25), delivery_date: refDay(-20), customer_key: 103, store_key: 303, product_key: 202, quantity: 2, unit_price: 149.50, net_price: 299.00, unit_cost: 65.00, currency_code: "USD", exchange_rate: 1.0 },
      { id: 5, order_number: 5003, line_number: 2, order_date: refDay(-25), delivery_date: refDay(-20), customer_key: 103, store_key: 303, product_key: 207, quantity: 1, unit_price: 79.99, net_price: 79.99, unit_cost: 32.00, currency_code: "USD", exchange_rate: 1.0 },
      { id: 6, order_number: 5004, line_number: 1, order_date: refDay(-20), delivery_date: refDay(-16), customer_key: 104, store_key: 304, product_key: 206, quantity: 1, unit_price: 1299.99, net_price: 1299.99, unit_cost: 650.00, currency_code: "USD", exchange_rate: 1.0 },
      { id: 7, order_number: 5005, line_number: 1, order_date: refDay(-14), delivery_date: refDay(-10), customer_key: 105, store_key: 305, product_key: 205, quantity: 1, unit_price: 399.00, net_price: 399.00, unit_cost: 180.00, currency_code: "USD", exchange_rate: 1.0 },
      { id: 8, order_number: 5006, line_number: 1, order_date: refDay(-10), delivery_date: refDay(-6),  customer_key: 106, store_key: 301, product_key: 201, quantity: 2, unit_price: 249.99, net_price: 499.98, unit_cost: 110.00, currency_code: "USD", exchange_rate: 1.0 },
      { id: 9, order_number: 5007, line_number: 1, order_date: refDay(-5),  delivery_date: refDay(-1),  customer_key: 107, store_key: 302, product_key: 208, quantity: 3, unit_price: 69.95, net_price: 209.85, unit_cost: 28.00, currency_code: "USD", exchange_rate: 1.0 },
      { id: 10, order_number: 5008, line_number: 1, order_date: refDay(-2),  delivery_date: refDay(2),   customer_key: 108, store_key: 303, product_key: 204, quantity: 1, unit_price: 119.99, net_price: 119.99, unit_cost: 45.00, currency_code: "USD", exchange_rate: 1.0 }
    ]
  }
);

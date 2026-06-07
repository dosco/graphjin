# GraphJin Database Schema for Webshop Example
# Use 'graphjin db diff' to preview changes and 'graphjin db sync' to apply

type users {
  id: BigInt! @id
  full_name: Varchar!
  phone: Varchar
  avatar: Varchar
  email: Varchar!
  encrypted_password: Varchar!
  reset_password_token: Varchar
  reset_password_sent_at: Timestamp
  remember_created_at: Timestamp
  category_counts: JSONB
  created_at: Timestamp!
  updated_at: Timestamp!
}

type customers {
  id: BigInt! @id
  full_name: Varchar!
  phone: Varchar
  stripe_id: Varchar
  email: Varchar!
  encrypted_password: Varchar!
  reset_password_token: Varchar
  reset_password_sent_at: Timestamp
  remember_created_at: Timestamp
  created_at: Timestamp!
  updated_at: Timestamp!
}

type categories {
  id: BigInt! @id
  name: Text!
  description: Text
  created_at: Timestamp!
  updated_at: Timestamp!
}

type products {
  id: BigInt! @id
  name: Text!
  description: Text
  tags: TextArray
  category_ids: BigIntArray!
  price: Numeric
  user_id: BigInt @relation(type: users, field: id)
  created_at: Timestamp!
  updated_at: Timestamp!
}

type purchases {
  id: BigInt! @id
  customer_id: BigInt @relation(type: customers, field: id)
  product_id: BigInt @relation(type: products, field: id)
  quantity: Integer
  returned_at: Timestamp
  created_at: Timestamp!
  updated_at: Timestamp!
}

type notifications {
  id: BigInt! @id
  key: Varchar
  subject_type: Varchar
  subject_id: BigInt
  user_id: BigInt @relation(type: users, field: id)
  created_at: Timestamp!
  updated_at: Timestamp!
}

type comments {
  id: BigInt! @id
  body: Text!
  product_id: BigInt @relation(type: products, field: id)
  user_id: BigInt @relation(type: users, field: id)
  reply_to_id: BigInt @relation(type: comments, field: id)
  created_at: Timestamp!
  updated_at: Timestamp!
}

type chats {
  id: BigInt! @id
  body: Text
  reply_to_id: BigIntArray
  created_at: Timestamp!
  updated_at: Timestamp!
}

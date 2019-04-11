# This file should contain all the record creation needed to seed the database with its default values.
# The data can then be loaded with the rails db:seed command (or created alongside the database with db:setup).
#
# Examples:
#
#   movies = Movie.create([{ name: 'Star Wars' }, { name: 'Lord of the Rings' }])
#   Character.create(name: 'Luke', movie: movies.first)

pwd = "123456"

user_count = 10
customer_count = 100
product_count = 50
purchase_count = 100

3.times do |i|
  user = User.create(
    full_name: Faker::Name.name,
    avatar: Faker::Avatar.image,
    phone: Faker::PhoneNumber.cell_phone,
    email: "user#{i}@demo.com",
    password: pwd,
    password_confirmation: pwd
  )
  user.save!
  puts user.inspect
end

user_count.times do |i|
  user = User.create(
    full_name: Faker::Name.name,
    avatar: Faker::Avatar.image,
    phone: Faker::PhoneNumber.cell_phone,
    email: Faker::Internet.email,
    password: pwd,
    password_confirmation: pwd
  )
  user.save!
  puts user.inspect
end

customer_count.times do |i|
  customer = Customer.create(
    full_name: Faker::Name.name,
    phone: Faker::PhoneNumber.cell_phone,
    email: Faker::Internet.email,
    password: pwd,
    password_confirmation: pwd
  )
  customer.save!
  puts customer.inspect
end

product_count.times do |i|
  desc = [
    Faker::Beer.brand,
    Faker::Beer.style, 
    Faker::Beer.hop,
    Faker::Beer.yeast,
    Faker::Beer.ibu,
    Faker::Beer.alcohol,
    Faker::Beer.blg,
  ].join(", ")

  product = Product.create(
    name: Faker::Beer.name,
    description: desc,
    price: rand(5.5..19.0),
    user_id: rand(1..user_count)
  )
  product.save!
  puts product.inspect
end

purchase_count.times do |i|
  sale_type = ["rented", "bought"].sample

  if sale_type == "rented"
    due_date = Faker::Date.forward(30)
    if [0, 3].sample == 1
      returned = Faker::Date.forward(60)
    end
  end

  purchase = Purchase.create(
    customer_id: rand(1..customer_count),
    product_id: rand(1..product_count),
    sale_type: sale_type,
    quantity: rand(1..10),
    due_date: due_date,
    returned: returned
  )
  purchase.save!
  puts purchase.inspect
end

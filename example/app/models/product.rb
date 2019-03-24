class Product < ApplicationRecord
  has_many :customers, through: :purchases
end

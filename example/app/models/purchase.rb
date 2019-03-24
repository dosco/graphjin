class Purchase < ApplicationRecord
  validates :sale_type, :inclusion => { :in => %w{rented bought} }
  validates :quantity, numericality: { greater_than: 0 }
end

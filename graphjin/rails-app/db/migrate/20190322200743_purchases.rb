class Purchases < ActiveRecord::Migration[5.2]
  def change
    create_table :purchases do |t|
      t.references :customer, foreign_key: true
      t.references :product, foreign_key: true
      t.string :sale_type
      t.integer :quantity
      t.datetime :due_date
      t.datetime :returned
    end
  end
end

class CreateProducts < ActiveRecord::Migration[5.2]
  def change
    create_table :products do |t|
      t.string :name
      t.text :description
      t.decimal :price, precision: 7, scale: 2
      t.belongs_to :user, foreign_key: true

      t.timestamps
    end
  end
end

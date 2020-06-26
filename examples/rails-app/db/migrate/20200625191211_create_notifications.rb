class CreateNotifications < ActiveRecord::Migration[5.2]
  def change
    create_table :notifications do |t|
      t.string :key
      t.string :subject_type
      t.references :subject      
      t.belongs_to :user, foreign_key: true

      t.timestamps
    end
  end
end

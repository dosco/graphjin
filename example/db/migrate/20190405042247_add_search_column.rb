class AddSearchColumn < ActiveRecord::Migration[5.1]
  def self.up
    add_column :products, :tsv, :tsvector
    add_index :products, :tsv, using: "gin"

    say_with_time("Adding trigger to update the ts_vector column") do
      execute <<-SQL
        CREATE FUNCTION products_tsv_trigger() RETURNS trigger AS $$
        begin
          new.tsv :=
          setweight(to_tsvector('pg_catalog.english', coalesce(new.name,'')), 'A') ||
          setweight(to_tsvector('pg_catalog.english', coalesce(new.description,'')), 'B');
          return new;
        end
        $$ LANGUAGE plpgsql;

        CREATE TRIGGER tsvectorupdate BEFORE INSERT OR UPDATE ON products FOR EACH ROW EXECUTE PROCEDURE products_tsv_trigger();
        SQL
      end
  end

  def self.down
    say_with_time("Removing trigger to update the tsv column") do
      execute <<-SQL
        DROP TRIGGER tsvectorupdate
        ON products
        SQL
    end

    remove_index :products, :tsv
    remove_column :products, :tsv
  end
end
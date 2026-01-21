-- SpatiaLite GIS schema for SQLite tests
-- This file is loaded only when SpatiaLite extension is available

-- Initialize SpatiaLite metadata tables
SELECT InitSpatialMetaData(1);

-- Create locations table
CREATE TABLE locations (
  id INTEGER PRIMARY KEY,
  name TEXT
);

-- Add geometry column using SpatiaLite function
SELECT AddGeometryColumn('locations', 'geom', 4326, 'POINT', 'XY');

-- Create spatial index
SELECT CreateSpatialIndex('locations', 'geom');

-- Insert test data (SpatiaLite uses lon/lat order like PostGIS)
INSERT INTO locations (id, name, geom) VALUES
  (1, 'San Francisco', MakePoint(-122.4194, 37.7749, 4326)),
  (2, 'Oakland', MakePoint(-122.2711, 37.8044, 4326)),
  (3, 'San Jose', MakePoint(-121.8853, 37.3382, 4326)),
  (4, 'Berkeley', MakePoint(-122.2727, 37.8716, 4326)),
  (5, 'Palo Alto', MakePoint(-122.1430, 37.4419, 4326));

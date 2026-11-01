-- AdventureWorks for Postgres
--  by Lorin Thwaits

-- How to use this script:

-- Download "Adventure Works 2014 OLTP Script" from:
--   https://msftdbprodsamples.codeplex.com/downloads/get/880662

-- Extract the .zip and copy all of the CSV files into the same folder containing
-- this install.sql file and the update_csvs.rb file.

-- Modify the CSVs to work with Postgres by running:
--   ruby update_csvs.rb

-- Create the database and tables, import the data, and set up the views and keys with:
--   psql -c "CREATE DATABASE \"Adventureworks\";"
--   psql -d Adventureworks < install.sql

-- All 68 tables are properly set up.
-- All 20 views are established.
-- 68 additional convenience views are added which:
--   * Provide a shorthand to refer to tables.
--   * Add an "id" column to a primary key or primary-ish key if it makes sense.
--
--   For example, with the convenience views you can simply do:
--       SELECT pe.p.firstname, hr.e.jobtitle
--       FROM pe.p
--         INNER JOIN hr.e ON pe.p.id = hr.e.id;
--   Instead of:
--       SELECT p.firstname, e.jobtitle
--       FROM person.person AS p
--         INNER JOIN humanresources.employee AS e ON p.businessentityid = e.businessentityid;
--
-- Schemas for these views:
--   pe = person
--   hr = humanresources
--   pr = production
--   pu = purchasing
--   sa = sales
-- Easily get a list of all of these with:  \dv (pe|hr|pr|pu|sa).*

-- Enjoy!


-- -- Disconnect all other existing connections
-- SELECT pg_terminate_backend(pid)
--   FROM pg_stat_activity
--   WHERE pid <> pg_backend_pid() AND datname='Adventureworks';

\pset tuples_only on

-- Support to auto-generate UUIDs (aka GUIDs)
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Support crosstab function to do PIVOT thing for Sales.vSalesPersonSalesByFiscalYears
CREATE EXTENSION tablefunc;

-------------------------------------
-- Custom data types
-------------------------------------

CREATE DOMAIN "OrderNumber" varchar(25) NULL;
CREATE DOMAIN "AccountNumber" varchar(15) NULL;

CREATE DOMAIN "Flag" boolean NOT NULL;
CREATE DOMAIN "NameStyle" boolean NOT NULL;
CREATE DOMAIN "Name" varchar(50) NULL;
CREATE DOMAIN "Phone" varchar(25) NULL;


-------------------------------------
-- Five schemas, with tables and data
-------------------------------------

CREATE SCHEMA Person
  CREATE TABLE BusinessEntity(
    BusinessEntityID SERIAL, --  NOT FOR REPLICATION
    rowguid uuid NOT NULL CONSTRAINT "DF_BusinessEntity_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_BusinessEntity_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE Person(
    BusinessEntityID INT NOT NULL,
    PersonType char(2) NOT NULL,
    NameStyle "NameStyle" NOT NULL CONSTRAINT "DF_Person_NameStyle" DEFAULT (false),
    Title varchar(8) NULL,
    FirstName "Name" NOT NULL,
    MiddleName "Name" NULL,
    LastName "Name" NOT NULL,
    Suffix varchar(10) NULL,
    EmailPromotion INT NOT NULL CONSTRAINT "DF_Person_EmailPromotion" DEFAULT (0),
    AdditionalContactInfo XML NULL, -- XML("AdditionalContactInfoSchemaCollection"),
    Demographics XML NULL, -- XML("IndividualSurveySchemaCollection"),
    rowguid uuid NOT NULL CONSTRAINT "DF_Person_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_Person_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_Person_EmailPromotion" CHECK (EmailPromotion BETWEEN 0 AND 2),
    CONSTRAINT "CK_Person_PersonType" CHECK (PersonType IS NULL OR UPPER(PersonType) IN ('SC', 'VC', 'IN', 'EM', 'SP', 'GC'))
  )
  CREATE TABLE StateProvince(
    StateProvinceID SERIAL,
    StateProvinceCode char(3) NOT NULL,
    CountryRegionCode varchar(3) NOT NULL,
    IsOnlyStateProvinceFlag "Flag" NOT NULL CONSTRAINT "DF_StateProvince_IsOnlyStateProvinceFlag" DEFAULT (true),
    Name "Name" NOT NULL,
    TerritoryID INT NOT NULL,
    rowguid uuid NOT NULL CONSTRAINT "DF_StateProvince_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_StateProvince_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE Address(
    AddressID SERIAL, --  NOT FOR REPLICATION
    AddressLine1 varchar(60) NOT NULL,
    AddressLine2 varchar(60) NULL,
    City varchar(30) NOT NULL,
    StateProvinceID INT NOT NULL,
    PostalCode varchar(15) NOT NULL,
    SpatialLocation bytea NULL,
    rowguid uuid NOT NULL CONSTRAINT "DF_Address_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_Address_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE AddressType(
    AddressTypeID SERIAL,
    Name "Name" NOT NULL,
    rowguid uuid NOT NULL CONSTRAINT "DF_AddressType_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_AddressType_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE BusinessEntityAddress(
    BusinessEntityID INT NOT NULL,
    AddressID INT NOT NULL,
    AddressTypeID INT NOT NULL,
    rowguid uuid NOT NULL CONSTRAINT "DF_BusinessEntityAddress_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_BusinessEntityAddress_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE ContactType(
    ContactTypeID SERIAL,
    Name "Name" NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ContactType_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE BusinessEntityContact(
    BusinessEntityID INT NOT NULL,
    PersonID INT NOT NULL,
    ContactTypeID INT NOT NULL,
    rowguid uuid NOT NULL CONSTRAINT "DF_BusinessEntityContact_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_BusinessEntityContact_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE EmailAddress(
    BusinessEntityID INT NOT NULL,
    EmailAddressID SERIAL,
    EmailAddress varchar(50) NULL,
    rowguid uuid NOT NULL CONSTRAINT "DF_EmailAddress_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_EmailAddress_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE Password(
    BusinessEntityID INT NOT NULL,
    PasswordHash VARCHAR(128) NOT NULL,
    PasswordSalt VARCHAR(10) NOT NULL,
    rowguid uuid NOT NULL CONSTRAINT "DF_Password_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_Password_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE PhoneNumberType(
    PhoneNumberTypeID SERIAL,
    Name "Name" NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_PhoneNumberType_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE PersonPhone(
    BusinessEntityID INT NOT NULL,
    PhoneNumber "Phone" NOT NULL,
    PhoneNumberTypeID INT NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_PersonPhone_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE CountryRegion(
    CountryRegionCode varchar(3) NOT NULL,
    Name "Name" NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_CountryRegion_ModifiedDate" DEFAULT (NOW())
  );

COMMENT ON SCHEMA Person IS 'Contains objects related to names and addresses of customers, vendors, and employees';

SELECT 'Copying data into Person.BusinessEntity';
\copy Person.BusinessEntity FROM './BusinessEntity.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Person.Person';
\copy Person.Person FROM './Person.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Person.StateProvince';
\copy Person.StateProvince FROM './StateProvince.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Person.Address';
\copy Person.Address FROM './Address.csv' DELIMITER E'\t' CSV ENCODING 'latin1';
SELECT 'Copying data into Person.AddressType';
\copy Person.AddressType FROM './AddressType.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Person.BusinessEntityAddress';
\copy Person.BusinessEntityAddress FROM './BusinessEntityAddress.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Person.ContactType';
\copy Person.ContactType FROM './ContactType.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Person.BusinessEntityContact';
\copy Person.BusinessEntityContact FROM './BusinessEntityContact.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Person.EmailAddress';
\copy Person.EmailAddress FROM './EmailAddress.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Person.Password';
\copy Person.Password FROM './Password.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Person.PhoneNumberType';
\copy Person.PhoneNumberType FROM './PhoneNumberType.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Person.PersonPhone';
\copy Person.PersonPhone FROM './PersonPhone.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Person.CountryRegion';
\copy Person.CountryRegion FROM './CountryRegion.csv' DELIMITER E'\t' CSV;


CREATE SCHEMA HumanResources
  CREATE TABLE Department(
    DepartmentID SERIAL NOT NULL, -- smallint
    Name "Name" NOT NULL,
    GroupName "Name" NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_Department_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE Employee(
    BusinessEntityID INT NOT NULL,
    NationalIDNumber varchar(15) NOT NULL,
    LoginID varchar(256) NOT NULL,    
    Org varchar NULL,-- hierarchyid, will become OrganizationNode
    OrganizationLevel INT NULL, -- AS OrganizationNode.GetLevel(),
    JobTitle varchar(50) NOT NULL,
    BirthDate DATE NOT NULL,
    MaritalStatus char(1) NOT NULL,
    Gender char(1) NOT NULL,
    HireDate DATE NOT NULL,
    SalariedFlag "Flag" NOT NULL CONSTRAINT "DF_Employee_SalariedFlag" DEFAULT (true),
    VacationHours smallint NOT NULL CONSTRAINT "DF_Employee_VacationHours" DEFAULT (0),
    SickLeaveHours smallint NOT NULL CONSTRAINT "DF_Employee_SickLeaveHours" DEFAULT (0),
    CurrentFlag "Flag" NOT NULL CONSTRAINT "DF_Employee_CurrentFlag" DEFAULT (true),
    rowguid uuid NOT NULL CONSTRAINT "DF_Employee_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_Employee_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_Employee_BirthDate" CHECK (BirthDate BETWEEN '1930-01-01' AND NOW() - INTERVAL '18 years'),
    CONSTRAINT "CK_Employee_MaritalStatus" CHECK (UPPER(MaritalStatus) IN ('M', 'S')), -- Married or Single
    CONSTRAINT "CK_Employee_HireDate" CHECK (HireDate BETWEEN '1996-07-01' AND NOW() + INTERVAL '1 day'),
    CONSTRAINT "CK_Employee_Gender" CHECK (UPPER(Gender) IN ('M', 'F')), -- Male or Female
    CONSTRAINT "CK_Employee_VacationHours" CHECK (VacationHours BETWEEN -40 AND 240),
    CONSTRAINT "CK_Employee_SickLeaveHours" CHECK (SickLeaveHours BETWEEN 0 AND 120)
  )
  CREATE TABLE EmployeeDepartmentHistory(
    BusinessEntityID INT NOT NULL,
    DepartmentID smallint NOT NULL,
    ShiftID smallint NOT NULL, -- tinyint
    StartDate DATE NOT NULL,
    EndDate DATE NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_EmployeeDepartmentHistory_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_EmployeeDepartmentHistory_EndDate" CHECK ((EndDate >= StartDate) OR (EndDate IS NULL))
  )
  CREATE TABLE EmployeePayHistory(
    BusinessEntityID INT NOT NULL,
    RateChangeDate TIMESTAMP NOT NULL,
    Rate numeric NOT NULL, -- money
    PayFrequency smallint NOT NULL,  -- tinyint
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_EmployeePayHistory_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_EmployeePayHistory_PayFrequency" CHECK (PayFrequency IN (1, 2)), -- 1 = monthly salary, 2 = biweekly salary
    CONSTRAINT "CK_EmployeePayHistory_Rate" CHECK (Rate BETWEEN 6.50 AND 200.00)
  )
  CREATE TABLE JobCandidate(
    JobCandidateID SERIAL NOT NULL, -- int
    BusinessEntityID INT NULL,
    Resume XML NULL, -- XML(HRResumeSchemaCollection)
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_JobCandidate_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE Shift(
    ShiftID SERIAL NOT NULL, -- tinyint
    Name "Name" NOT NULL,
    StartTime time NOT NULL,
    EndTime time NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_Shift_ModifiedDate" DEFAULT (NOW())
  );

COMMENT ON SCHEMA HumanResources IS 'Contains objects related to employees and departments.';

SELECT 'Copying data into HumanResources.Department';
\copy HumanResources.Department FROM './Department.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into HumanResources.Employee';
\copy HumanResources.Employee FROM './Employee.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into HumanResources.EmployeeDepartmentHistory';
\copy HumanResources.EmployeeDepartmentHistory FROM './EmployeeDepartmentHistory.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into HumanResources.EmployeePayHistory';
\copy HumanResources.EmployeePayHistory FROM './EmployeePayHistory.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into HumanResources.JobCandidate';
\copy HumanResources.JobCandidate FROM './JobCandidate.csv' DELIMITER E'\t' CSV ENCODING 'latin1';
SELECT 'Copying data into HumanResources.Shift';
\copy HumanResources.Shift FROM './Shift.csv' DELIMITER E'\t' CSV;

-- Calculated column that needed to be there just for the CSV import
ALTER TABLE HumanResources.Employee DROP COLUMN OrganizationLevel;

-- Employee HierarchyID column
ALTER TABLE HumanResources.Employee ADD organizationnode VARCHAR DEFAULT '/';
-- Convert from all the hex to a stream of hierarchyid bits
WITH RECURSIVE hier AS (
  SELECT businessentityid, org, get_byte(decode(substring(org, 1, 2), 'hex'), 0)::bit(8)::varchar AS bits, 2 AS i
    FROM HumanResources.Employee
  UNION ALL
  SELECT e.businessentityid, e.org, hier.bits || get_byte(decode(substring(e.org, i + 1, 2), 'hex'), 0)::bit(8)::varchar, i + 2 AS i
    FROM HumanResources.Employee AS e INNER JOIN
      hier ON e.businessentityid = hier.businessentityid AND i < LENGTH(e.org)
)
UPDATE HumanResources.Employee AS emp
  SET org = COALESCE(trim(trailing '0' FROM hier.bits::TEXT), '')
  FROM hier
  WHERE emp.businessentityid = hier.businessentityid
    AND (hier.org IS NULL OR i = LENGTH(hier.org));

-- Convert bits to the real hieararchy paths
CREATE OR REPLACE FUNCTION f_ConvertOrgNodes()
  RETURNS void AS
$func$
DECLARE
  got_none BOOLEAN;
BEGIN
  LOOP
  got_none := true;
  -- 01 = 0-3
  UPDATE HumanResources.Employee
   SET organizationnode = organizationnode || SUBSTRING(org, 3,2)::bit(2)::INTEGER::VARCHAR || CASE SUBSTRING(org, 5, 1) WHEN '0' THEN '.' ELSE '/' END,
     org = SUBSTRING(org, 6, 9999)
    WHERE org LIKE '01%';
  IF FOUND THEN
    got_none := false;
  END IF;

  -- 100 = 4-7
  UPDATE HumanResources.Employee
   SET organizationnode = organizationnode || (SUBSTRING(org, 4,2)::bit(2)::INTEGER + 4)::VARCHAR || CASE SUBSTRING(org, 6, 1) WHEN '0' THEN '.' ELSE '/' END,
     org = SUBSTRING(org, 7, 9999)
    WHERE org LIKE '100%';
  IF FOUND THEN
    got_none := false;
  END IF;
  
  -- 101 = 8-15
  UPDATE HumanResources.Employee
   SET organizationnode = organizationnode || (SUBSTRING(org, 4,3)::bit(3)::INTEGER + 8)::VARCHAR || CASE SUBSTRING(org, 7, 1) WHEN '0' THEN '.' ELSE '/' END,
     org = SUBSTRING(org, 8, 9999)
    WHERE org LIKE '101%';
  IF FOUND THEN
    got_none := false;
  END IF;

  -- 110 = 16-79
  UPDATE HumanResources.Employee
   SET organizationnode = organizationnode || ((SUBSTRING(org, 4,2)||SUBSTRING(org, 7,1)||SUBSTRING(org, 9,3))::bit(6)::INTEGER + 16)::VARCHAR || CASE SUBSTRING(org, 12, 1) WHEN '0' THEN '.' ELSE '/' END,
     org = SUBSTRING(org, 13, 9999)
    WHERE org LIKE '110%';
  IF FOUND THEN
    got_none := false;
  END IF;

  -- 1110 = 80-1103
  UPDATE HumanResources.Employee
   SET organizationnode = organizationnode || ((SUBSTRING(org, 5,3)||SUBSTRING(org, 9,3)||SUBSTRING(org, 13,1)||SUBSTRING(org, 15,3))::bit(10)::INTEGER + 80)::VARCHAR || CASE SUBSTRING(org, 18, 1) WHEN '0' THEN '.' ELSE '/' END,
     org = SUBSTRING(org, 19, 9999)
    WHERE org LIKE '1110%';
  IF FOUND THEN
    got_none := false;
  END IF;
  EXIT WHEN got_none;
  END LOOP;
END
$func$ LANGUAGE plpgsql;

SELECT f_ConvertOrgNodes();
-- Drop the original binary hierarchyid column
ALTER TABLE HumanResources.Employee DROP COLUMN Org;
DROP FUNCTION f_ConvertOrgNodes();




CREATE SCHEMA Production
  CREATE TABLE BillOfMaterials(
    BillOfMaterialsID SERIAL NOT NULL, -- int
    ProductAssemblyID INT NULL,
    ComponentID INT NOT NULL,
    StartDate TIMESTAMP NOT NULL CONSTRAINT "DF_BillOfMaterials_StartDate" DEFAULT (NOW()),
    EndDate TIMESTAMP NULL,
    UnitMeasureCode char(3) NOT NULL,
    BOMLevel smallint NOT NULL,
    PerAssemblyQty decimal(8, 2) NOT NULL CONSTRAINT "DF_BillOfMaterials_PerAssemblyQty" DEFAULT (1.00),
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_BillOfMaterials_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_BillOfMaterials_EndDate" CHECK ((EndDate > StartDate) OR (EndDate IS NULL)),
    CONSTRAINT "CK_BillOfMaterials_ProductAssemblyID" CHECK (ProductAssemblyID <> ComponentID),
    CONSTRAINT "CK_BillOfMaterials_BOMLevel" CHECK (((ProductAssemblyID IS NULL)
        AND (BOMLevel = 0) AND (PerAssemblyQty = 1.00))
        OR ((ProductAssemblyID IS NOT NULL) AND (BOMLevel >= 1))),
    CONSTRAINT "CK_BillOfMaterials_PerAssemblyQty" CHECK (PerAssemblyQty >= 1.00)
  )
  CREATE TABLE Culture(
    CultureID char(6) NOT NULL,
    Name "Name" NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_Culture_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE Document(
    Doc varchar NULL,-- hierarchyid, will become DocumentNode
    DocumentLevel INTEGER, -- AS DocumentNode.GetLevel(),
    Title varchar(50) NOT NULL,
    Owner INT NOT NULL,
    FolderFlag "Flag" NOT NULL CONSTRAINT "DF_Document_FolderFlag" DEFAULT (false),
    FileName varchar(400) NOT NULL,
    FileExtension varchar(8) NULL,
    Revision char(5) NOT NULL,
    ChangeNumber INT NOT NULL CONSTRAINT "DF_Document_ChangeNumber" DEFAULT (0),
    Status smallint NOT NULL, -- tinyint
    DocumentSummary text NULL,
    Document bytea  NULL, -- varbinary
    rowguid uuid NOT NULL UNIQUE CONSTRAINT "DF_Document_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_Document_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_Document_Status" CHECK (Status BETWEEN 1 AND 3)
  )
  CREATE TABLE ProductCategory(
    ProductCategoryID SERIAL NOT NULL, -- int
    Name "Name" NOT NULL,
    rowguid uuid NOT NULL CONSTRAINT "DF_ProductCategory_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ProductCategory_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE ProductSubcategory(
    ProductSubcategoryID SERIAL NOT NULL, -- int
    ProductCategoryID INT NOT NULL,
    Name "Name" NOT NULL,
    rowguid uuid NOT NULL CONSTRAINT "DF_ProductSubcategory_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ProductSubcategory_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE ProductModel(
    ProductModelID SERIAL NOT NULL, -- int
    Name "Name" NOT NULL,
    CatalogDescription XML NULL, -- XML(Production.ProductDescriptionSchemaCollection)
    Instructions XML NULL, -- XML(Production.ManuInstructionsSchemaCollection)
    rowguid uuid NOT NULL CONSTRAINT "DF_ProductModel_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ProductModel_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE Product(
    ProductID SERIAL NOT NULL, -- int
    Name "Name" NOT NULL,
    ProductNumber varchar(25) NOT NULL,
    MakeFlag "Flag" NOT NULL CONSTRAINT "DF_Product_MakeFlag" DEFAULT (true),
    FinishedGoodsFlag "Flag" NOT NULL CONSTRAINT "DF_Product_FinishedGoodsFlag" DEFAULT (true),
    Color varchar(15) NULL,
    SafetyStockLevel smallint NOT NULL,
    ReorderPoint smallint NOT NULL,
    StandardCost numeric NOT NULL, -- money
    ListPrice numeric NOT NULL, -- money
    Size varchar(5) NULL,
    SizeUnitMeasureCode char(3) NULL,
    WeightUnitMeasureCode char(3) NULL,
    Weight decimal(8, 2) NULL,
    DaysToManufacture INT NOT NULL,
    ProductLine char(2) NULL,
    Class char(2) NULL,
    Style char(2) NULL,
    ProductSubcategoryID INT NULL,
    ProductModelID INT NULL,
    SellStartDate TIMESTAMP NOT NULL,
    SellEndDate TIMESTAMP NULL,
    DiscontinuedDate TIMESTAMP NULL,
    rowguid uuid NOT NULL CONSTRAINT "DF_Product_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_Product_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_Product_SafetyStockLevel" CHECK (SafetyStockLevel > 0),
    CONSTRAINT "CK_Product_ReorderPoint" CHECK (ReorderPoint > 0),
    CONSTRAINT "CK_Product_StandardCost" CHECK (StandardCost >= 0.00),
    CONSTRAINT "CK_Product_ListPrice" CHECK (ListPrice >= 0.00),
    CONSTRAINT "CK_Product_Weight" CHECK (Weight > 0.00),
    CONSTRAINT "CK_Product_DaysToManufacture" CHECK (DaysToManufacture >= 0),
    CONSTRAINT "CK_Product_ProductLine" CHECK (UPPER(ProductLine) IN ('S', 'T', 'M', 'R') OR ProductLine IS NULL),
    CONSTRAINT "CK_Product_Class" CHECK (UPPER(Class) IN ('L', 'M', 'H') OR Class IS NULL),
    CONSTRAINT "CK_Product_Style" CHECK (UPPER(Style) IN ('W', 'M', 'U') OR Style IS NULL),
    CONSTRAINT "CK_Product_SellEndDate" CHECK ((SellEndDate >= SellStartDate) OR (SellEndDate IS NULL))
  )
  CREATE TABLE ProductCostHistory(
    ProductID INT NOT NULL,
    StartDate TIMESTAMP NOT NULL,
    EndDate TIMESTAMP NULL,
    StandardCost numeric NOT NULL,  -- money
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ProductCostHistory_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_ProductCostHistory_EndDate" CHECK ((EndDate >= StartDate) OR (EndDate IS NULL)),
    CONSTRAINT "CK_ProductCostHistory_StandardCost" CHECK (StandardCost >= 0.00)
  )
  CREATE TABLE ProductDescription(
    ProductDescriptionID SERIAL NOT NULL, -- int
    Description varchar(400) NOT NULL,
    rowguid uuid NOT NULL CONSTRAINT "DF_ProductDescription_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ProductDescription_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE ProductDocument(
    ProductID INT NOT NULL,
    Doc varchar NOT NULL, -- hierarchyid, will become DocumentNode
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ProductDocument_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE Location(
    LocationID SERIAL NOT NULL, -- smallint
    Name "Name" NOT NULL,
    CostRate numeric NOT NULL CONSTRAINT "DF_Location_CostRate" DEFAULT (0.00), -- smallmoney -- money
    Availability decimal(8, 2) NOT NULL CONSTRAINT "DF_Location_Availability" DEFAULT (0.00),
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_Location_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_Location_CostRate" CHECK (CostRate >= 0.00),
    CONSTRAINT "CK_Location_Availability" CHECK (Availability >= 0.00)
  )
  CREATE TABLE ProductInventory(
    ProductID INT NOT NULL,
    LocationID smallint NOT NULL,
    Shelf varchar(10) NOT NULL,
    Bin smallint NOT NULL, -- tinyint
    Quantity smallint NOT NULL CONSTRAINT "DF_ProductInventory_Quantity" DEFAULT (0),
    rowguid uuid NOT NULL CONSTRAINT "DF_ProductInventory_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ProductInventory_ModifiedDate" DEFAULT (NOW()),
--    CONSTRAINT "CK_ProductInventory_Shelf" CHECK ((Shelf LIKE 'AZa-z]') OR (Shelf = 'N/A')),
    CONSTRAINT "CK_ProductInventory_Bin" CHECK (Bin BETWEEN 0 AND 100)
  )
  CREATE TABLE ProductListPriceHistory(
    ProductID INT NOT NULL,
    StartDate TIMESTAMP NOT NULL,
    EndDate TIMESTAMP NULL,
    ListPrice numeric NOT NULL,  -- money
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ProductListPriceHistory_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_ProductListPriceHistory_EndDate" CHECK ((EndDate >= StartDate) OR (EndDate IS NULL)),
    CONSTRAINT "CK_ProductListPriceHistory_ListPrice" CHECK (ListPrice > 0.00)
  )
  CREATE TABLE Illustration(
    IllustrationID SERIAL NOT NULL, -- int
    Diagram XML NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_Illustration_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE ProductModelIllustration(
    ProductModelID INT NOT NULL,
    IllustrationID INT NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ProductModelIllustration_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE ProductModelProductDescriptionCulture(
    ProductModelID INT NOT NULL,
    ProductDescriptionID INT NOT NULL,
    CultureID char(6) NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ProductModelProductDescriptionCulture_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE ProductPhoto(
    ProductPhotoID SERIAL NOT NULL, -- int
    ThumbNailPhoto bytea NULL,-- varbinary
    ThumbnailPhotoFileName varchar(50) NULL,
    LargePhoto bytea NULL,-- varbinary
    LargePhotoFileName varchar(50) NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ProductPhoto_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE ProductProductPhoto(
    ProductID INT NOT NULL,
    ProductPhotoID INT NOT NULL,
    "primary" "Flag" NOT NULL CONSTRAINT "DF_ProductProductPhoto_Primary" DEFAULT (false),
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ProductProductPhoto_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE ProductReview(
    ProductReviewID SERIAL NOT NULL, -- int
    ProductID INT NOT NULL,
    ReviewerName "Name" NOT NULL,
    ReviewDate TIMESTAMP NOT NULL CONSTRAINT "DF_ProductReview_ReviewDate" DEFAULT (NOW()),
    EmailAddress varchar(50) NOT NULL,
    Rating INT NOT NULL,
    Comments varchar(3850),
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ProductReview_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_ProductReview_Rating" CHECK (Rating BETWEEN 1 AND 5)
  )
  CREATE TABLE ScrapReason(
    ScrapReasonID SERIAL NOT NULL, -- smallint
    Name "Name" NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ScrapReason_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE TransactionHistory(
    TransactionID SERIAL NOT NULL, -- INT IDENTITY (100000, 1)
    ProductID INT NOT NULL,
    ReferenceOrderID INT NOT NULL,
    ReferenceOrderLineID INT NOT NULL CONSTRAINT "DF_TransactionHistory_ReferenceOrderLineID" DEFAULT (0),
    TransactionDate TIMESTAMP NOT NULL CONSTRAINT "DF_TransactionHistory_TransactionDate" DEFAULT (NOW()),
    TransactionType char(1) NOT NULL,
    Quantity INT NOT NULL,
    ActualCost numeric NOT NULL,  -- money
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_TransactionHistory_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_TransactionHistory_TransactionType" CHECK (UPPER(TransactionType) IN ('W', 'S', 'P'))
  )
  CREATE TABLE TransactionHistoryArchive(
    TransactionID INT NOT NULL,
    ProductID INT NOT NULL,
    ReferenceOrderID INT NOT NULL,
    ReferenceOrderLineID INT NOT NULL CONSTRAINT "DF_TransactionHistoryArchive_ReferenceOrderLineID" DEFAULT (0),
    TransactionDate TIMESTAMP NOT NULL CONSTRAINT "DF_TransactionHistoryArchive_TransactionDate" DEFAULT (NOW()),
    TransactionType char(1) NOT NULL,
    Quantity INT NOT NULL,
    ActualCost numeric NOT NULL,  -- money
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_TransactionHistoryArchive_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_TransactionHistoryArchive_TransactionType" CHECK (UPPER(TransactionType) IN ('W', 'S', 'P'))
  )
  CREATE TABLE UnitMeasure(
    UnitMeasureCode char(3) NOT NULL,
    Name "Name" NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_UnitMeasure_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE WorkOrder(
    WorkOrderID SERIAL NOT NULL, -- int
    ProductID INT NOT NULL,
    OrderQty INT NOT NULL,
    StockedQty INT, -- AS ISNULL(OrderQty - ScrappedQty, 0),
    ScrappedQty smallint NOT NULL,
    StartDate TIMESTAMP NOT NULL,
    EndDate TIMESTAMP NULL,
    DueDate TIMESTAMP NOT NULL,
    ScrapReasonID smallint NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_WorkOrder_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_WorkOrder_OrderQty" CHECK (OrderQty > 0),
    CONSTRAINT "CK_WorkOrder_ScrappedQty" CHECK (ScrappedQty >= 0),
    CONSTRAINT "CK_WorkOrder_EndDate" CHECK ((EndDate >= StartDate) OR (EndDate IS NULL))
  )
  CREATE TABLE WorkOrderRouting(
    WorkOrderID INT NOT NULL,
    ProductID INT NOT NULL,
    OperationSequence smallint NOT NULL,
    LocationID smallint NOT NULL,
    ScheduledStartDate TIMESTAMP NOT NULL,
    ScheduledEndDate TIMESTAMP NOT NULL,
    ActualStartDate TIMESTAMP NULL,
    ActualEndDate TIMESTAMP NULL,
    ActualResourceHrs decimal(9, 4) NULL,
    PlannedCost numeric NOT NULL, -- money
    ActualCost numeric NULL,  -- money
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_WorkOrderRouting_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_WorkOrderRouting_ScheduledEndDate" CHECK (ScheduledEndDate >= ScheduledStartDate),
    CONSTRAINT "CK_WorkOrderRouting_ActualEndDate" CHECK ((ActualEndDate >= ActualStartDate)
        OR (ActualEndDate IS NULL) OR (ActualStartDate IS NULL)),
    CONSTRAINT "CK_WorkOrderRouting_ActualResourceHrs" CHECK (ActualResourceHrs >= 0.0000),
    CONSTRAINT "CK_WorkOrderRouting_PlannedCost" CHECK (PlannedCost > 0.00),
    CONSTRAINT "CK_WorkOrderRouting_ActualCost" CHECK (ActualCost > 0.00)
  );

COMMENT ON SCHEMA Production IS 'Contains objects related to products, inventory, and manufacturing.';

SELECT 'Copying data into Production.BillOfMaterials';
\copy Production.BillOfMaterials FROM 'BillOfMaterials.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.Culture';
\copy Production.Culture FROM 'Culture.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.Document';
\copy Production.Document FROM 'Document.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.ProductCategory';
\copy Production.ProductCategory FROM 'ProductCategory.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.ProductSubcategory';
\copy Production.ProductSubcategory FROM 'ProductSubcategory.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.ProductModel';
\copy Production.ProductModel FROM 'ProductModel.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.Product';
\copy Production.Product FROM 'Product.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.ProductCostHistory';
\copy Production.ProductCostHistory FROM 'ProductCostHistory.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.ProductDescription';
\copy Production.ProductDescription FROM 'ProductDescription.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.ProductDocument';
\copy Production.ProductDocument FROM 'ProductDocument.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.Location';
\copy Production.Location FROM 'Location.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.ProductInventory';
\copy Production.ProductInventory FROM 'ProductInventory.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.ProductListPriceHistory';
\copy Production.ProductListPriceHistory FROM 'ProductListPriceHistory.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.Illustration';
\copy Production.Illustration FROM 'Illustration.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.ProductModelIllustration';
\copy Production.ProductModelIllustration FROM 'ProductModelIllustration.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.ProductModelProductDescriptionCulture';
\copy Production.ProductModelProductDescriptionCulture FROM 'ProductModelProductDescriptionCulture.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.ProductPhoto';
\copy Production.ProductPhoto FROM 'ProductPhoto.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.ProductProductPhoto';
\copy Production.ProductProductPhoto FROM 'ProductProductPhoto.csv' DELIMITER E'\t' CSV;

-- This doesn't work:
-- SELECT 'Copying data into Production.ProductReview';
-- \copy Production.ProductReview FROM 'ProductReview.csv' DELIMITER '  ' CSV;

-- so instead ...
INSERT INTO Production.ProductReview (ProductReviewID, ProductID, ReviewerName, ReviewDate, EmailAddress, Rating, Comments, ModifiedDate) VALUES
 (1, 709, 'John Smith', '2013-09-18 00:00:00', 'john@fourthcoffee.com', 5, 'I can''t believe I''m singing the praises of a pair of socks, but I just came back from a grueling
3-day ride and these socks really helped make the trip a blast. They''re lightweight yet really cushioned my feet all day. 
The reinforced toe is nearly bullet-proof and I didn''t experience any problems with rubbing or blisters like I have with
other brands. I know it sounds silly, but it''s always the little stuff (like comfortable feet) that makes or breaks a long trip.
I won''t go on another trip without them!', '2013-09-18 00:00:00'),

 (2, 937, 'David', '2013-11-13 00:00:00', 'david@graphicdesigninstitute.com', 4, 'A little on the heavy side, but overall the entry/exit is easy in all conditions. I''ve used these pedals for 
more than 3 years and I''ve never had a problem. Cleanup is easy. Mud and sand don''t get trapped. I would like 
them even better if there was a weight reduction. Maybe in the next design. Still, I would recommend them to a friend.', '2013-11-13 00:00:00'),

 (3, 937, 'Jill', '2013-11-15 00:00:00', 'jill@margiestravel.com', 2, 'Maybe it''s just because I''m new to mountain biking, but I had a terrible time getting use
to these pedals. In my first outing, I wiped out trying to release my foot. Any suggestions on
ways I can adjust the pedals, or is it just a learning curve thing?', '2013-11-15 00:00:00'),

 (4, 798, 'Laura Norman', '2013-11-15 00:00:00', 'laura@treyresearch.net', 5, 'The Road-550-W from Adventure Works Cycles is everything it''s advertised to be. Finally, a quality bike that
is actually built for a woman and provides control and comfort in one neat package. The top tube is shorter, the suspension is weight-tuned and there''s a much shorter reach to the brake
levers. All this adds up to a great mountain bike that is sure to accommodate any woman''s anatomy. In addition to getting the size right, the saddle is incredibly comfortable. 
Attention to detail is apparent in every aspect from the frame finish to the careful design of each component. Each component is a solid performer without any fluff. 
The designers clearly did their homework and thought about size, weight, and funtionality throughout. And at less than 19 pounds, the bike is manageable for even the most petite cyclist.

We had 5 riders take the bike out for a spin and really put it to the test. The results were consistent and very positive. Our testers loved the manuverability 
and control they had with the redesigned frame on the 550-W. A definite improvement over the 2012 design. Four out of five testers listed quick handling
and responsivness were the key elements they noticed. Technical climbing and on the flats, the bike just cruises through the rough. Tight corners and obstacles were handled effortlessly. The fifth tester was more impressed with the smooth ride. The heavy-duty shocks absorbed even the worst bumps and provided a soft ride on all but the 
nastiest trails and biggest drops. The shifting was rated superb and typical of what we''ve come to expect from Adventure Works Cycles. On descents, the bike handled flawlessly and tracked very well. The bike is well balanced front-to-rear and frame flex was minimal. In particular, the testers
noted that the brake system had a unique combination of power and modulation.  While some brake setups can be overly touchy, these brakes had a good
amount of power, but also a good feel that allows you to apply as little or as much braking power as is needed. Second is their short break-in period. We found that they tend to break-in well before
the end of the first ride; while others take two to three rides (or more) to come to full power. 

On the negative side, the pedals were not quite up to our tester''s standards. 
Just for fun, we experimented with routine maintenance tasks. Overall we found most operations to be straight forward and easy to complete. The only exception was replacing the front wheel. The maintenance manual that comes
with the bike say to install the front wheel with the axle quick release or bolt, then compress the fork a few times before fastening and tightening the two quick-release mechanisms on the bottom of the dropouts. This is to seat the axle in the dropouts, and if you do not
do this, the axle will become seated after you tightened the two bottom quick releases, which will then become loose. It''s better to test the tightness carefully or you may notice that the two bottom quick releases have come loose enough to fall completely open. And that''s something you don''t want to experience
while out on the road! 

The Road-550-W frame is available in a variety of sizes and colors and has the same durable, high-quality aluminum that AWC is known for. At a MSRP of just under $1125.00, it''s comparable in price to its closest competitors and
we think that after a test drive you''l find the quality and performance above and beyond . You''ll have a grin on your face and be itching to get out on the road for more. While designed for serious road racing, the Road-550-W would be an excellent choice for just about any terrain and 
any level of experience. It''s a huge step in the right direction for female cyclists and well worth your consideration and hard-earned money.', '2013-11-15 00:00:00');

SELECT 'Copying data into Production.ScrapReason';
\copy Production.ScrapReason FROM 'ScrapReason.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.TransactionHistory';
\copy Production.TransactionHistory FROM 'TransactionHistory.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.TransactionHistoryArchive';
\copy Production.TransactionHistoryArchive FROM 'TransactionHistoryArchive.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.UnitMeasure';
\copy Production.UnitMeasure FROM 'UnitMeasure.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.WorkOrder';
\copy Production.WorkOrder FROM 'WorkOrder.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Production.WorkOrderRouting';
\copy Production.WorkOrderRouting FROM 'WorkOrderRouting.csv' DELIMITER E'\t' CSV;

-- Calculated columns that needed to be there just for the CSV import
ALTER TABLE Production.WorkOrder DROP COLUMN StockedQty;
ALTER TABLE Production.Document DROP COLUMN DocumentLevel;

-- Document HierarchyID column
ALTER TABLE Production.Document ADD DocumentNode VARCHAR DEFAULT '/';
-- Convert from all the hex to a stream of hierarchyid bits
WITH RECURSIVE hier AS (
  SELECT rowguid, doc, get_byte(decode(substring(doc, 1, 2), 'hex'), 0)::bit(8)::varchar AS bits, 2 AS i
    FROM Production.Document
  UNION ALL
  SELECT e.rowguid, e.doc, hier.bits || get_byte(decode(substring(e.doc, i + 1, 2), 'hex'), 0)::bit(8)::varchar, i + 2 AS i
    FROM Production.Document AS e INNER JOIN
      hier ON e.rowguid = hier.rowguid AND i < LENGTH(e.doc)
)
UPDATE Production.Document AS emp
  SET doc = COALESCE(trim(trailing '0' FROM hier.bits::TEXT), '')
  FROM hier
  WHERE emp.rowguid = hier.rowguid
    AND (hier.doc IS NULL OR i = LENGTH(hier.doc));

-- Convert bits to the real hieararchy paths
CREATE OR REPLACE FUNCTION f_ConvertDocNodes()
  RETURNS void AS
$func$
DECLARE
  got_none BOOLEAN;
BEGIN
  LOOP
  got_none := true;
  -- 01 = 0-3
  UPDATE Production.Document
   SET DocumentNode = DocumentNode || SUBSTRING(doc, 3,2)::bit(2)::INTEGER::VARCHAR || CASE SUBSTRING(doc, 5, 1) WHEN '0' THEN '.' ELSE '/' END,
     doc = SUBSTRING(doc, 6, 9999)
    WHERE doc LIKE '01%';
  IF FOUND THEN
    got_none := false;
  END IF;

  -- 100 = 4-7
  UPDATE Production.Document
   SET DocumentNode = DocumentNode || (SUBSTRING(doc, 4,2)::bit(2)::INTEGER + 4)::VARCHAR || CASE SUBSTRING(doc, 6, 1) WHEN '0' THEN '.' ELSE '/' END,
     doc = SUBSTRING(doc, 7, 9999)
    WHERE doc LIKE '100%';
  IF FOUND THEN
    got_none := false;
  END IF;
  
  -- 101 = 8-15
  UPDATE Production.Document
   SET DocumentNode = DocumentNode || (SUBSTRING(doc, 4,3)::bit(3)::INTEGER + 8)::VARCHAR || CASE SUBSTRING(doc, 7, 1) WHEN '0' THEN '.' ELSE '/' END,
     doc = SUBSTRING(doc, 8, 9999)
    WHERE doc LIKE '101%';
  IF FOUND THEN
    got_none := false;
  END IF;

  -- 110 = 16-79
  UPDATE Production.Document
   SET DocumentNode = DocumentNode || ((SUBSTRING(doc, 4,2)||SUBSTRING(doc, 7,1)||SUBSTRING(doc, 9,3))::bit(6)::INTEGER + 16)::VARCHAR || CASE SUBSTRING(doc, 12, 1) WHEN '0' THEN '.' ELSE '/' END,
     doc = SUBSTRING(doc, 13, 9999)
    WHERE doc LIKE '110%';
  IF FOUND THEN
    got_none := false;
  END IF;

  -- 1110 = 80-1103
  UPDATE Production.Document
   SET DocumentNode = DocumentNode || ((SUBSTRING(doc, 5,3)||SUBSTRING(doc, 9,3)||SUBSTRING(doc, 13,1)||SUBSTRING(doc, 15,3))::bit(10)::INTEGER + 80)::VARCHAR || CASE SUBSTRING(doc, 18, 1) WHEN '0' THEN '.' ELSE '/' END,
     doc = SUBSTRING(doc, 19, 9999)
    WHERE doc LIKE '1110%';
  IF FOUND THEN
    got_none := false;
  END IF;
  EXIT WHEN got_none;
  END LOOP;
END
$func$ LANGUAGE plpgsql;

SELECT f_ConvertDocNodes();
-- Drop the original binary hierarchyid column
ALTER TABLE Production.Document DROP COLUMN Doc;
DROP FUNCTION f_ConvertDocNodes();

-- ProductDocument HierarchyID column
  ALTER TABLE Production.ProductDocument ADD DocumentNode VARCHAR DEFAULT '/';
ALTER TABLE Production.ProductDocument ADD rowguid uuid NOT NULL CONSTRAINT "DF_ProductDocument_rowguid" DEFAULT (uuid_generate_v1());
-- Convert from all the hex to a stream of hierarchyid bits
WITH RECURSIVE hier AS (
  SELECT rowguid, doc, get_byte(decode(substring(doc, 1, 2), 'hex'), 0)::bit(8)::varchar AS bits, 2 AS i
    FROM Production.ProductDocument
  UNION ALL
  SELECT e.rowguid, e.doc, hier.bits || get_byte(decode(substring(e.doc, i + 1, 2), 'hex'), 0)::bit(8)::varchar, i + 2 AS i
    FROM Production.ProductDocument AS e INNER JOIN
      hier ON e.rowguid = hier.rowguid AND i < LENGTH(e.doc)
)
UPDATE Production.ProductDocument AS emp
  SET doc = COALESCE(trim(trailing '0' FROM hier.bits::TEXT), '')
  FROM hier
  WHERE emp.rowguid = hier.rowguid
    AND (hier.doc IS NULL OR i = LENGTH(hier.doc));

-- Convert bits to the real hieararchy paths
CREATE OR REPLACE FUNCTION f_ConvertDocNodes()
  RETURNS void AS
$func$
DECLARE
  got_none BOOLEAN;
BEGIN
  LOOP
  got_none := true;
  -- 01 = 0-3
  UPDATE Production.ProductDocument
   SET DocumentNode = DocumentNode || SUBSTRING(doc, 3,2)::bit(2)::INTEGER::VARCHAR || CASE SUBSTRING(doc, 5, 1) WHEN '0' THEN '.' ELSE '/' END,
     doc = SUBSTRING(doc, 6, 9999)
    WHERE doc LIKE '01%';
  IF FOUND THEN
    got_none := false;
  END IF;

  -- 100 = 4-7
  UPDATE Production.ProductDocument
   SET DocumentNode = DocumentNode || (SUBSTRING(doc, 4,2)::bit(2)::INTEGER + 4)::VARCHAR || CASE SUBSTRING(doc, 6, 1) WHEN '0' THEN '.' ELSE '/' END,
     doc = SUBSTRING(doc, 7, 9999)
    WHERE doc LIKE '100%';
  IF FOUND THEN
    got_none := false;
  END IF;
  
  -- 101 = 8-15
  UPDATE Production.ProductDocument
   SET DocumentNode = DocumentNode || (SUBSTRING(doc, 4,3)::bit(3)::INTEGER + 8)::VARCHAR || CASE SUBSTRING(doc, 7, 1) WHEN '0' THEN '.' ELSE '/' END,
     doc = SUBSTRING(doc, 8, 9999)
    WHERE doc LIKE '101%';
  IF FOUND THEN
    got_none := false;
  END IF;

  -- 110 = 16-79
  UPDATE Production.ProductDocument
   SET DocumentNode = DocumentNode || ((SUBSTRING(doc, 4,2)||SUBSTRING(doc, 7,1)||SUBSTRING(doc, 9,3))::bit(6)::INTEGER + 16)::VARCHAR || CASE SUBSTRING(doc, 12, 1) WHEN '0' THEN '.' ELSE '/' END,
     doc = SUBSTRING(doc, 13, 9999)
    WHERE doc LIKE '110%';
  IF FOUND THEN
    got_none := false;
  END IF;

  -- 1110 = 80-1103
  UPDATE Production.ProductDocument
   SET DocumentNode = DocumentNode || ((SUBSTRING(doc, 5,3)||SUBSTRING(doc, 9,3)||SUBSTRING(doc, 13,1)||SUBSTRING(doc, 15,3))::bit(10)::INTEGER + 80)::VARCHAR || CASE SUBSTRING(doc, 18, 1) WHEN '0' THEN '.' ELSE '/' END,
     doc = SUBSTRING(doc, 19, 9999)
    WHERE doc LIKE '1110%';
  IF FOUND THEN
    got_none := false;
  END IF;
  EXIT WHEN got_none;
  END LOOP;
END
$func$ LANGUAGE plpgsql;

SELECT f_ConvertDocNodes();
-- Drop the original binary hierarchyid column
ALTER TABLE Production.ProductDocument DROP COLUMN Doc;
DROP FUNCTION f_ConvertDocNodes();
ALTER TABLE Production.ProductDocument DROP COLUMN rowguid;





CREATE SCHEMA Purchasing
  CREATE TABLE ProductVendor(
    ProductID INT NOT NULL,
    BusinessEntityID INT NOT NULL,
    AverageLeadTime INT NOT NULL,
    StandardPrice numeric NOT NULL, -- money
    LastReceiptCost numeric NULL, -- money
    LastReceiptDate TIMESTAMP NULL,
    MinOrderQty INT NOT NULL,
    MaxOrderQty INT NOT NULL,
    OnOrderQty INT NULL,
    UnitMeasureCode char(3) NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ProductVendor_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_ProductVendor_AverageLeadTime" CHECK (AverageLeadTime >= 1),
    CONSTRAINT "CK_ProductVendor_StandardPrice" CHECK (StandardPrice > 0.00),
    CONSTRAINT "CK_ProductVendor_LastReceiptCost" CHECK (LastReceiptCost > 0.00),
    CONSTRAINT "CK_ProductVendor_MinOrderQty" CHECK (MinOrderQty >= 1),
    CONSTRAINT "CK_ProductVendor_MaxOrderQty" CHECK (MaxOrderQty >= 1),
    CONSTRAINT "CK_ProductVendor_OnOrderQty" CHECK (OnOrderQty >= 0)
  )
  CREATE TABLE PurchaseOrderDetail(
    PurchaseOrderID INT NOT NULL,
    PurchaseOrderDetailID SERIAL NOT NULL, -- int
    DueDate TIMESTAMP NOT NULL,
    OrderQty smallint NOT NULL,
    ProductID INT NOT NULL,
    UnitPrice numeric NOT NULL, -- money
    LineTotal numeric, -- AS ISNULL(OrderQty * UnitPrice, 0.00),
    ReceivedQty decimal(8, 2) NOT NULL,
    RejectedQty decimal(8, 2) NOT NULL,
    StockedQty numeric, -- AS ISNULL(ReceivedQty - RejectedQty, 0.00),
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_PurchaseOrderDetail_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_PurchaseOrderDetail_OrderQty" CHECK (OrderQty > 0),
    CONSTRAINT "CK_PurchaseOrderDetail_UnitPrice" CHECK (UnitPrice >= 0.00),
    CONSTRAINT "CK_PurchaseOrderDetail_ReceivedQty" CHECK (ReceivedQty >= 0.00),
    CONSTRAINT "CK_PurchaseOrderDetail_RejectedQty" CHECK (RejectedQty >= 0.00)
  )
  CREATE TABLE PurchaseOrderHeader(
    PurchaseOrderID SERIAL NOT NULL,  -- int
    RevisionNumber smallint NOT NULL CONSTRAINT "DF_PurchaseOrderHeader_RevisionNumber" DEFAULT (0),  -- tinyint
    Status smallint NOT NULL CONSTRAINT "DF_PurchaseOrderHeader_Status" DEFAULT (1),  -- tinyint
    EmployeeID INT NOT NULL,
    VendorID INT NOT NULL,
    ShipMethodID INT NOT NULL,
    OrderDate TIMESTAMP NOT NULL CONSTRAINT "DF_PurchaseOrderHeader_OrderDate" DEFAULT (NOW()),
    ShipDate TIMESTAMP NULL,
    SubTotal numeric NOT NULL CONSTRAINT "DF_PurchaseOrderHeader_SubTotal" DEFAULT (0.00),  -- money
    TaxAmt numeric NOT NULL CONSTRAINT "DF_PurchaseOrderHeader_TaxAmt" DEFAULT (0.00),  -- money
    Freight numeric NOT NULL CONSTRAINT "DF_PurchaseOrderHeader_Freight" DEFAULT (0.00),  -- money
    TotalDue numeric, -- AS ISNULL(SubTotal + TaxAmt + Freight, 0) PERSISTED NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_PurchaseOrderHeader_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_PurchaseOrderHeader_Status" CHECK (Status BETWEEN 1 AND 4), -- 1 = Pending; 2 = Approved; 3 = Rejected; 4 = Complete
    CONSTRAINT "CK_PurchaseOrderHeader_ShipDate" CHECK ((ShipDate >= OrderDate) OR (ShipDate IS NULL)),
    CONSTRAINT "CK_PurchaseOrderHeader_SubTotal" CHECK (SubTotal >= 0.00),
    CONSTRAINT "CK_PurchaseOrderHeader_TaxAmt" CHECK (TaxAmt >= 0.00),
    CONSTRAINT "CK_PurchaseOrderHeader_Freight" CHECK (Freight >= 0.00)
  )
  CREATE TABLE ShipMethod(
    ShipMethodID SERIAL NOT NULL, -- int
    Name "Name" NOT NULL,
    ShipBase numeric NOT NULL CONSTRAINT "DF_ShipMethod_ShipBase" DEFAULT (0.00), -- money
    ShipRate numeric NOT NULL CONSTRAINT "DF_ShipMethod_ShipRate" DEFAULT (0.00), -- money
    rowguid uuid NOT NULL CONSTRAINT "DF_ShipMethod_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ShipMethod_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_ShipMethod_ShipBase" CHECK (ShipBase > 0.00),
    CONSTRAINT "CK_ShipMethod_ShipRate" CHECK (ShipRate > 0.00)
  )
  CREATE TABLE Vendor(
    BusinessEntityID INT NOT NULL,
    AccountNumber "AccountNumber" NOT NULL,
    Name "Name" NOT NULL,
    CreditRating smallint NOT NULL, -- tinyint
    PreferredVendorStatus "Flag" NOT NULL CONSTRAINT "DF_Vendor_PreferredVendorStatus" DEFAULT (true),
    ActiveFlag "Flag" NOT NULL CONSTRAINT "DF_Vendor_ActiveFlag" DEFAULT (true),
    PurchasingWebServiceURL varchar(1024) NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_Vendor_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_Vendor_CreditRating" CHECK (CreditRating BETWEEN 1 AND 5)
  );

COMMENT ON SCHEMA Purchasing IS 'Contains objects related to vendors and purchase orders.';

SELECT 'Copying data into Purchasing.ProductVendor';
\copy Purchasing.ProductVendor FROM 'ProductVendor.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Purchasing.PurchaseOrderDetail';
\copy Purchasing.PurchaseOrderDetail FROM 'PurchaseOrderDetail.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Purchasing.PurchaseOrderHeader';
\copy Purchasing.PurchaseOrderHeader FROM 'PurchaseOrderHeader.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Purchasing.ShipMethod';
\copy Purchasing.ShipMethod FROM 'ShipMethod.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Purchasing.Vendor';
\copy Purchasing.Vendor FROM 'Vendor.csv' DELIMITER E'\t' CSV;

-- Calculated columns that needed to be there just for the CSV import
ALTER TABLE Purchasing.PurchaseOrderDetail DROP COLUMN LineTotal;
ALTER TABLE Purchasing.PurchaseOrderDetail DROP COLUMN StockedQty;
ALTER TABLE Purchasing.PurchaseOrderHeader DROP COLUMN TotalDue;



CREATE SCHEMA Sales
  CREATE TABLE CountryRegionCurrency(
    CountryRegionCode varchar(3) NOT NULL,
    CurrencyCode char(3) NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_CountryRegionCurrency_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE CreditCard(
    CreditCardID SERIAL NOT NULL, -- int
    CardType varchar(50) NOT NULL,
    CardNumber varchar(25) NOT NULL,
    ExpMonth smallint NOT NULL, -- tinyint
    ExpYear smallint NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_CreditCard_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE Currency(
    CurrencyCode char(3) NOT NULL,
    Name "Name" NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_Currency_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE CurrencyRate(
    CurrencyRateID SERIAL NOT NULL, -- int
    CurrencyRateDate TIMESTAMP NOT NULL,   
    FromCurrencyCode char(3) NOT NULL,
    ToCurrencyCode char(3) NOT NULL,
    AverageRate numeric NOT NULL, -- money
    EndOfDayRate numeric NOT NULL,  -- money
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_CurrencyRate_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE Customer(
    CustomerID SERIAL NOT NULL, --  NOT FOR REPLICATION -- int
    -- A customer may either be a person, a store, or a person who works for a store
    PersonID INT NULL, -- If this customer represents a person, this is non-null
    StoreID INT NULL,  -- If the customer is a store, or is associated with a store then this is non-null.
    TerritoryID INT NULL,
    AccountNumber VARCHAR, -- AS ISNULL('AW' + dbo.ufnLeadingZeros(CustomerID), ''),
    rowguid uuid NOT NULL CONSTRAINT "DF_Customer_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_Customer_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE PersonCreditCard(
    BusinessEntityID INT NOT NULL,
    CreditCardID INT NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_PersonCreditCard_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE SalesOrderDetail(
    SalesOrderID INT NOT NULL,
    SalesOrderDetailID SERIAL NOT NULL, -- int
    CarrierTrackingNumber varchar(25) NULL,
    OrderQty smallint NOT NULL,
    ProductID INT NOT NULL,
    SpecialOfferID INT NOT NULL,
    UnitPrice numeric NOT NULL, -- money
    UnitPriceDiscount numeric NOT NULL CONSTRAINT "DF_SalesOrderDetail_UnitPriceDiscount" DEFAULT (0.0), -- money
    LineTotal numeric, -- AS ISNULL(UnitPrice * (1.0 - UnitPriceDiscount) * OrderQty, 0.0),
    rowguid uuid NOT NULL CONSTRAINT "DF_SalesOrderDetail_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_SalesOrderDetail_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_SalesOrderDetail_OrderQty" CHECK (OrderQty > 0),
    CONSTRAINT "CK_SalesOrderDetail_UnitPrice" CHECK (UnitPrice >= 0.00),
    CONSTRAINT "CK_SalesOrderDetail_UnitPriceDiscount" CHECK (UnitPriceDiscount >= 0.00)
  )
  CREATE TABLE SalesOrderHeader(
    SalesOrderID SERIAL NOT NULL, --  NOT FOR REPLICATION -- int
    RevisionNumber smallint NOT NULL CONSTRAINT "DF_SalesOrderHeader_RevisionNumber" DEFAULT (0), -- tinyint
    OrderDate TIMESTAMP NOT NULL CONSTRAINT "DF_SalesOrderHeader_OrderDate" DEFAULT (NOW()),
    DueDate TIMESTAMP NOT NULL,
    ShipDate TIMESTAMP NULL,
    Status smallint NOT NULL CONSTRAINT "DF_SalesOrderHeader_Status" DEFAULT (1), -- tinyint
    OnlineOrderFlag "Flag" NOT NULL CONSTRAINT "DF_SalesOrderHeader_OnlineOrderFlag" DEFAULT (true),
    SalesOrderNumber VARCHAR(23), -- AS ISNULL(N'SO' + CONVERT(nvarchar(23), SalesOrderID), N'*** ERROR ***'),
    PurchaseOrderNumber "OrderNumber" NULL,
    AccountNumber "AccountNumber" NULL,
    CustomerID INT NOT NULL,
    SalesPersonID INT NULL,
    TerritoryID INT NULL,
    BillToAddressID INT NOT NULL,
    ShipToAddressID INT NOT NULL,
    ShipMethodID INT NOT NULL,
    CreditCardID INT NULL,
    CreditCardApprovalCode varchar(15) NULL,   
    CurrencyRateID INT NULL,
    SubTotal numeric NOT NULL CONSTRAINT "DF_SalesOrderHeader_SubTotal" DEFAULT (0.00), -- money
    TaxAmt numeric NOT NULL CONSTRAINT "DF_SalesOrderHeader_TaxAmt" DEFAULT (0.00), -- money
    Freight numeric NOT NULL CONSTRAINT "DF_SalesOrderHeader_Freight" DEFAULT (0.00), -- money
    TotalDue numeric, -- AS ISNULL(SubTotal + TaxAmt + Freight, 0),
    Comment varchar(128) NULL,
    rowguid uuid NOT NULL CONSTRAINT "DF_SalesOrderHeader_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_SalesOrderHeader_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_SalesOrderHeader_Status" CHECK (Status BETWEEN 0 AND 8),
    CONSTRAINT "CK_SalesOrderHeader_DueDate" CHECK (DueDate >= OrderDate),
    CONSTRAINT "CK_SalesOrderHeader_ShipDate" CHECK ((ShipDate >= OrderDate) OR (ShipDate IS NULL)),
    CONSTRAINT "CK_SalesOrderHeader_SubTotal" CHECK (SubTotal >= 0.00),
    CONSTRAINT "CK_SalesOrderHeader_TaxAmt" CHECK (TaxAmt >= 0.00),
    CONSTRAINT "CK_SalesOrderHeader_Freight" CHECK (Freight >= 0.00)
  )
  CREATE TABLE SalesOrderHeaderSalesReason(
    SalesOrderID INT NOT NULL,
    SalesReasonID INT NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_SalesOrderHeaderSalesReason_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE SalesPerson(
    BusinessEntityID INT NOT NULL,
    TerritoryID INT NULL,
    SalesQuota numeric NULL, -- money
    Bonus numeric NOT NULL CONSTRAINT "DF_SalesPerson_Bonus" DEFAULT (0.00), -- money
    CommissionPct numeric NOT NULL CONSTRAINT "DF_SalesPerson_CommissionPct" DEFAULT (0.00), -- smallmoney -- money
    SalesYTD numeric NOT NULL CONSTRAINT "DF_SalesPerson_SalesYTD" DEFAULT (0.00), -- money
    SalesLastYear numeric NOT NULL CONSTRAINT "DF_SalesPerson_SalesLastYear" DEFAULT (0.00), -- money
    rowguid uuid NOT NULL CONSTRAINT "DF_SalesPerson_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_SalesPerson_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_SalesPerson_SalesQuota" CHECK (SalesQuota > 0.00),
    CONSTRAINT "CK_SalesPerson_Bonus" CHECK (Bonus >= 0.00),
    CONSTRAINT "CK_SalesPerson_CommissionPct" CHECK (CommissionPct >= 0.00),
    CONSTRAINT "CK_SalesPerson_SalesYTD" CHECK (SalesYTD >= 0.00),
    CONSTRAINT "CK_SalesPerson_SalesLastYear" CHECK (SalesLastYear >= 0.00)
  )
  CREATE TABLE SalesPersonQuotaHistory(
    BusinessEntityID INT NOT NULL,
    QuotaDate TIMESTAMP NOT NULL,
    SalesQuota numeric NOT NULL, -- money
    rowguid uuid NOT NULL CONSTRAINT "DF_SalesPersonQuotaHistory_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_SalesPersonQuotaHistory_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_SalesPersonQuotaHistory_SalesQuota" CHECK (SalesQuota > 0.00)
  )
  CREATE TABLE SalesReason(
    SalesReasonID SERIAL NOT NULL, -- int
    Name "Name" NOT NULL,
    ReasonType "Name" NOT NULL,
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_SalesReason_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE SalesTaxRate(
    SalesTaxRateID SERIAL NOT NULL, -- int
    StateProvinceID INT NOT NULL,
    TaxType smallint NOT NULL, -- tinyint
    TaxRate numeric NOT NULL CONSTRAINT "DF_SalesTaxRate_TaxRate" DEFAULT (0.00), -- smallmoney -- money
    Name "Name" NOT NULL,
    rowguid uuid NOT NULL CONSTRAINT "DF_SalesTaxRate_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_SalesTaxRate_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_SalesTaxRate_TaxType" CHECK (TaxType BETWEEN 1 AND 3)
  )
  CREATE TABLE SalesTerritory(
    TerritoryID SERIAL NOT NULL, -- int
    Name "Name" NOT NULL,
    CountryRegionCode varchar(3) NOT NULL,
    "group" varchar(50) NOT NULL, -- Group
    SalesYTD numeric NOT NULL CONSTRAINT "DF_SalesTerritory_SalesYTD" DEFAULT (0.00), -- money
    SalesLastYear numeric NOT NULL CONSTRAINT "DF_SalesTerritory_SalesLastYear" DEFAULT (0.00), -- money
    CostYTD numeric NOT NULL CONSTRAINT "DF_SalesTerritory_CostYTD" DEFAULT (0.00), -- money
    CostLastYear numeric NOT NULL CONSTRAINT "DF_SalesTerritory_CostLastYear" DEFAULT (0.00), -- money
    rowguid uuid NOT NULL CONSTRAINT "DF_SalesTerritory_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_SalesTerritory_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_SalesTerritory_SalesYTD" CHECK (SalesYTD >= 0.00),
    CONSTRAINT "CK_SalesTerritory_SalesLastYear" CHECK (SalesLastYear >= 0.00),
    CONSTRAINT "CK_SalesTerritory_CostYTD" CHECK (CostYTD >= 0.00),
    CONSTRAINT "CK_SalesTerritory_CostLastYear" CHECK (CostLastYear >= 0.00)
  )
  CREATE TABLE SalesTerritoryHistory(
    BusinessEntityID INT NOT NULL,  -- A sales person
    TerritoryID INT NOT NULL,
    StartDate TIMESTAMP NOT NULL,
    EndDate TIMESTAMP NULL,
    rowguid uuid NOT NULL CONSTRAINT "DF_SalesTerritoryHistory_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_SalesTerritoryHistory_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_SalesTerritoryHistory_EndDate" CHECK ((EndDate >= StartDate) OR (EndDate IS NULL))
  )
  CREATE TABLE ShoppingCartItem(
    ShoppingCartItemID SERIAL NOT NULL, -- int
    ShoppingCartID varchar(50) NOT NULL,
    Quantity INT NOT NULL CONSTRAINT "DF_ShoppingCartItem_Quantity" DEFAULT (1),
    ProductID INT NOT NULL,
    DateCreated TIMESTAMP NOT NULL CONSTRAINT "DF_ShoppingCartItem_DateCreated" DEFAULT (NOW()),
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_ShoppingCartItem_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_ShoppingCartItem_Quantity" CHECK (Quantity >= 1)
  )
  CREATE TABLE SpecialOffer(
    SpecialOfferID SERIAL NOT NULL, -- int
    Description varchar(255) NOT NULL,
    DiscountPct numeric NOT NULL CONSTRAINT "DF_SpecialOffer_DiscountPct" DEFAULT (0.00), -- smallmoney -- money
    Type varchar(50) NOT NULL,
    Category varchar(50) NOT NULL,
    StartDate TIMESTAMP NOT NULL,
    EndDate TIMESTAMP NOT NULL,
    MinQty INT NOT NULL CONSTRAINT "DF_SpecialOffer_MinQty" DEFAULT (0),
    MaxQty INT NULL,
    rowguid uuid NOT NULL CONSTRAINT "DF_SpecialOffer_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_SpecialOffer_ModifiedDate" DEFAULT (NOW()),
    CONSTRAINT "CK_SpecialOffer_EndDate" CHECK (EndDate >= StartDate),
    CONSTRAINT "CK_SpecialOffer_DiscountPct" CHECK (DiscountPct >= 0.00),
    CONSTRAINT "CK_SpecialOffer_MinQty" CHECK (MinQty >= 0),
    CONSTRAINT "CK_SpecialOffer_MaxQty"  CHECK (MaxQty >= 0)
  )
  CREATE TABLE SpecialOfferProduct(
    SpecialOfferID INT NOT NULL,
    ProductID INT NOT NULL,
    rowguid uuid NOT NULL CONSTRAINT "DF_SpecialOfferProduct_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_SpecialOfferProduct_ModifiedDate" DEFAULT (NOW())
  )
  CREATE TABLE Store(
    BusinessEntityID INT NOT NULL,
    Name "Name" NOT NULL,
    SalesPersonID INT NULL,
    Demographics XML NULL, -- XML(Sales.StoreSurveySchemaCollection)
    rowguid uuid NOT NULL CONSTRAINT "DF_Store_rowguid" DEFAULT (uuid_generate_v1()), -- ROWGUIDCOL
    ModifiedDate TIMESTAMP NOT NULL CONSTRAINT "DF_Store_ModifiedDate" DEFAULT (NOW())
  );

COMMENT ON SCHEMA Sales IS 'Contains objects related to customers, sales orders, and sales territories.';

SELECT 'Copying data into Sales.CountryRegionCurrency';
\copy Sales.CountryRegionCurrency FROM 'CountryRegionCurrency.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.CreditCard';
\copy Sales.CreditCard FROM 'CreditCard.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.Currency';
\copy Sales.Currency FROM 'Currency.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.CurrencyRate';
\copy Sales.CurrencyRate FROM 'CurrencyRate.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.Customer';
\copy Sales.Customer FROM 'Customer.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.PersonCreditCard';
\copy Sales.PersonCreditCard FROM 'PersonCreditCard.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.SalesOrderDetail';
\copy Sales.SalesOrderDetail FROM 'SalesOrderDetail.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.SalesOrderHeader';
\copy Sales.SalesOrderHeader FROM 'SalesOrderHeader.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.SalesOrderHeaderSalesReason';
\copy Sales.SalesOrderHeaderSalesReason FROM 'SalesOrderHeaderSalesReason.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.SalesPerson';
\copy Sales.SalesPerson FROM 'SalesPerson.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.SalesPersonQuotaHistory';
\copy Sales.SalesPersonQuotaHistory FROM 'SalesPersonQuotaHistory.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.SalesReason';
\copy Sales.SalesReason FROM 'SalesReason.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.SalesTaxRate';
\copy Sales.SalesTaxRate FROM 'SalesTaxRate.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.SalesTerritory';
\copy Sales.SalesTerritory FROM 'SalesTerritory.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.SalesTerritoryHistory';
\copy Sales.SalesTerritoryHistory FROM 'SalesTerritoryHistory.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.ShoppingCartItem';
\copy Sales.ShoppingCartItem FROM 'ShoppingCartItem.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.SpecialOffer';
\copy Sales.SpecialOffer FROM 'SpecialOffer.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.SpecialOfferProduct';
\copy Sales.SpecialOfferProduct FROM 'SpecialOfferProduct.csv' DELIMITER E'\t' CSV;
SELECT 'Copying data into Sales.Store';
\copy Sales.Store FROM 'Store.csv' DELIMITER E'\t' CSV;

-- Calculated columns that needed to be there just for the CSV import
ALTER TABLE Sales.Customer DROP COLUMN AccountNumber;
ALTER TABLE Sales.SalesOrderDetail DROP COLUMN LineTotal;
ALTER TABLE Sales.SalesOrderHeader DROP COLUMN SalesOrderNumber;



-------------------------------------
-- TABLE AND COLUMN COMMENTS
-------------------------------------

SET CLIENT_ENCODING=latin1;

-- COMMENT ON TABLE dbo.AWBuildVersion IS 'Current version number of the AdventureWorks2012_CS sample database.';
--   COMMENT ON COLUMN dbo.AWBuildVersion.SystemInformationID IS 'Primary key for AWBuildVersion records.';
--   COMMENT ON COLUMN AWBui.COLU.Version IS 'Version number of the database in 9.yy.mm.dd.00 format.';
--   COMMENT ON COLUMN dbo.AWBuildVersion.VersionDate IS 'Date and time the record was last updated.';

-- COMMENT ON TABLE dbo.DatabaseLog IS 'Audit table tracking all DDL changes made to the AdventureWorks database. Data is captured by the database trigger ddlDatabaseTriggerLog.';
--   COMMENT ON COLUMN dbo.DatabaseLog.PostTime IS 'The date and time the DDL change occurred.';
--   COMMENT ON COLUMN dbo.DatabaseLog.DatabaseUser IS 'The user who implemented the DDL change.';
--   COMMENT ON COLUMN dbo.DatabaseLog.Event IS 'The type of DDL statement that was executed.';
--   COMMENT ON COLUMN dbo.DatabaseLog.Schema IS 'The schema to which the changed object belongs.';
--   COMMENT ON COLUMN dbo.DatabaseLog.Object IS 'The object that was changed by the DDL statment.';
--   COMMENT ON COLUMN dbo.DatabaseLog.TSQL IS 'The exact Transact-SQL statement that was executed.';
--   COMMENT ON COLUMN dbo.DatabaseLog.XmlEvent IS 'The raw XML data generated by database trigger.';

-- COMMENT ON TABLE dbo.ErrorLog IS 'Audit table tracking errors in the the AdventureWorks database that are caught by the CATCH block of a TRY...CATCH construct. Data is inserted by stored procedure dbo.uspLogError when it is executed from inside the CATCH block of a TRY...CATCH construct.';
--   COMMENT ON COLUMN dbo.ErrorLog.ErrorLogID IS 'Primary key for ErrorLog records.';
--   COMMENT ON COLUMN dbo.ErrorLog.ErrorTime IS 'The date and time at which the error occurred.';
--   COMMENT ON COLUMN dbo.ErrorLog.UserName IS 'The user who executed the batch in which the error occurred.';
--   COMMENT ON COLUMN dbo.ErrorLog.ErrorNumber IS 'The error number of the error that occurred.';
--   COMMENT ON COLUMN dbo.ErrorLog.ErrorSeverity IS 'The severity of the error that occurred.';
--   COMMENT ON COLUMN dbo.ErrorLog.ErrorState IS 'The state number of the error that occurred.';
--   COMMENT ON COLUMN dbo.ErrorLog.ErrorProcedure IS 'The name of the stored procedure or trigger where the error occurred.';
--   COMMENT ON COLUMN dbo.ErrorLog.ErrorLine IS 'The line number at which the error occurred.';
--   COMMENT ON COLUMN dbo.ErrorLog.ErrorMessage IS 'The message text of the error that occurred.';

COMMENT ON TABLE Person.Address IS 'Street address information for customers, employees, and vendors.';
  COMMENT ON COLUMN Person.Address.AddressID IS 'Primary key for Address records.';
  COMMENT ON COLUMN Person.Address.AddressLine1 IS 'First street address line.';
  COMMENT ON COLUMN Person.Address.AddressLine2 IS 'Second street address line.';
  COMMENT ON COLUMN Person.Address.City IS 'Name of the city.';
  COMMENT ON COLUMN Person.Address.StateProvinceID IS 'Unique identification number for the state or province. Foreign key to StateProvince table.';
  COMMENT ON COLUMN Person.Address.PostalCode IS 'Postal code for the street address.';
  COMMENT ON COLUMN Person.Address.SpatialLocation IS 'Latitude and longitude of this address.';

COMMENT ON TABLE Person.AddressType IS 'Types of addresses stored in the Address table.';
  COMMENT ON COLUMN Person.AddressType.AddressTypeID IS 'Primary key for AddressType records.';
  COMMENT ON COLUMN Person.AddressType.Name IS 'Address type description. For example, Billing, Home, or Shipping.';

COMMENT ON TABLE Production.BillOfMaterials IS 'Items required to make bicycles and bicycle subassemblies. It identifies the heirarchical relationship between a parent product and its components.';
  COMMENT ON COLUMN Production.BillOfMaterials.BillOfMaterialsID IS 'Primary key for BillOfMaterials records.';
  COMMENT ON COLUMN Production.BillOfMaterials.ProductAssemblyID IS 'Parent product identification number. Foreign key to Product.ProductID.';
  COMMENT ON COLUMN Production.BillOfMaterials.ComponentID IS 'Component identification number. Foreign key to Product.ProductID.';
  COMMENT ON COLUMN Production.BillOfMaterials.StartDate IS 'Date the component started being used in the assembly item.';
  COMMENT ON COLUMN Production.BillOfMaterials.EndDate IS 'Date the component stopped being used in the assembly item.';
  COMMENT ON COLUMN Production.BillOfMaterials.UnitMeasureCode IS 'Standard code identifying the unit of measure for the quantity.';
  COMMENT ON COLUMN Production.BillOfMaterials.BOMLevel IS 'Indicates the depth the component is from its parent (AssemblyID).';
  COMMENT ON COLUMN Production.BillOfMaterials.PerAssemblyQty IS 'Quantity of the component needed to create the assembly.';

COMMENT ON TABLE Person.BusinessEntity IS 'Source of the ID that connects vendors, customers, and employees with address and contact information.';
  COMMENT ON COLUMN Person.BusinessEntity.BusinessEntityID IS 'Primary key for all customers, vendors, and employees.';

COMMENT ON TABLE Person.BusinessEntityAddress IS 'Cross-reference table mapping customers, vendors, and employees to their addresses.';
  COMMENT ON COLUMN Person.BusinessEntityAddress.BusinessEntityID IS 'Primary key. Foreign key to BusinessEntity.BusinessEntityID.';
  COMMENT ON COLUMN Person.BusinessEntityAddress.AddressID IS 'Primary key. Foreign key to Address.AddressID.';
  COMMENT ON COLUMN Person.BusinessEntityAddress.AddressTypeID IS 'Primary key. Foreign key to AddressType.AddressTypeID.';

COMMENT ON TABLE Person.BusinessEntityContact IS 'Cross-reference table mapping stores, vendors, and employees to people';
  COMMENT ON COLUMN Person.BusinessEntityContact.BusinessEntityID IS 'Primary key. Foreign key to BusinessEntity.BusinessEntityID.';
  COMMENT ON COLUMN Person.BusinessEntityContact.PersonID IS 'Primary key. Foreign key to Person.BusinessEntityID.';
  COMMENT ON COLUMN Person.BusinessEntityContact.ContactTypeID IS 'Primary key.  Foreign key to ContactType.ContactTypeID.';

COMMENT ON TABLE Person.ContactType IS 'Lookup table containing the types of business entity contacts.';
  COMMENT ON COLUMN Person.ContactType.ContactTypeID IS 'Primary key for ContactType records.';
  COMMENT ON COLUMN Person.ContactType.Name IS 'Contact type description.';

COMMENT ON TABLE Sales.CountryRegionCurrency IS 'Cross-reference table mapping ISO currency codes to a country or region.';
  COMMENT ON COLUMN Sales.CountryRegionCurrency.CountryRegionCode IS 'ISO code for countries and regions. Foreign key to CountryRegion.CountryRegionCode.';
  COMMENT ON COLUMN Sales.CountryRegionCurrency.CurrencyCode IS 'ISO standard currency code. Foreign key to Currency.CurrencyCode.';

COMMENT ON TABLE Person.CountryRegion IS 'Lookup table containing the ISO standard codes for countries and regions.';
  COMMENT ON COLUMN Person.CountryRegion.CountryRegionCode IS 'ISO standard code for countries and regions.';
  COMMENT ON COLUMN Person.CountryRegion.Name IS 'Country or region name.';

COMMENT ON TABLE Sales.CreditCard IS 'Customer credit card information.';
  COMMENT ON COLUMN Sales.CreditCard.CreditCardID IS 'Primary key for CreditCard records.';
  COMMENT ON COLUMN Sales.CreditCard.CardType IS 'Credit card name.';
  COMMENT ON COLUMN Sales.CreditCard.CardNumber IS 'Credit card number.';
  COMMENT ON COLUMN Sales.CreditCard.ExpMonth IS 'Credit card expiration month.';
  COMMENT ON COLUMN Sales.CreditCard.ExpYear IS 'Credit card expiration year.';

COMMENT ON TABLE Production.Culture IS 'Lookup table containing the languages in which some AdventureWorks data is stored.';
  COMMENT ON COLUMN Production.Culture.CultureID IS 'Primary key for Culture records.';
  COMMENT ON COLUMN Production.Culture.Name IS 'Culture description.';

COMMENT ON TABLE Sales.Currency IS 'Lookup table containing standard ISO currencies.';
  COMMENT ON COLUMN Sales.Currency.CurrencyCode IS 'The ISO code for the Currency.';
  COMMENT ON COLUMN Sales.Currency.Name IS 'Currency name.';

COMMENT ON TABLE Sales.CurrencyRate IS 'Currency exchange rates.';
  COMMENT ON COLUMN Sales.CurrencyRate.CurrencyRateID IS 'Primary key for CurrencyRate records.';
  COMMENT ON COLUMN Sales.CurrencyRate.CurrencyRateDate IS 'Date and time the exchange rate was obtained.';
  COMMENT ON COLUMN Sales.CurrencyRate.FromCurrencyCode IS 'Exchange rate was converted from this currency code.';
  COMMENT ON COLUMN Sales.CurrencyRate.ToCurrencyCode IS 'Exchange rate was converted to this currency code.';
  COMMENT ON COLUMN Sales.CurrencyRate.AverageRate IS 'Average exchange rate for the day.';
  COMMENT ON COLUMN Sales.CurrencyRate.EndOfDayRate IS 'Final exchange rate for the day.';

COMMENT ON TABLE Sales.Customer IS 'Current customer information. Also see the Person and Store tables.';
  COMMENT ON COLUMN Sales.Customer.CustomerID IS 'Primary key.';
  COMMENT ON COLUMN Sales.Customer.PersonID IS 'Foreign key to Person.BusinessEntityID';
  COMMENT ON COLUMN Sales.Customer.StoreID IS 'Foreign key to Store.BusinessEntityID';
  COMMENT ON COLUMN Sales.Customer.TerritoryID IS 'ID of the territory in which the customer is located. Foreign key to SalesTerritory.SalesTerritoryID.';
--  COMMENT ON COLUMN Sales.Customer.AccountNumber IS 'Unique number identifying the customer assigned by the accounting system.';

COMMENT ON TABLE HumanResources.Department IS 'Lookup table containing the departments within the Adventure Works Cycles company.';
  COMMENT ON COLUMN HumanResources.Department.DepartmentID IS 'Primary key for Department records.';
  COMMENT ON COLUMN HumanResources.Department.Name IS 'Name of the department.';
  COMMENT ON COLUMN HumanResources.Department.GroupName IS 'Name of the group to which the department belongs.';

COMMENT ON TABLE Production.Document IS 'Product maintenance documents.';
  COMMENT ON COLUMN Production.Document.DocumentNode IS 'Primary key for Document records.';
--  COMMENT ON COLUMN Production.Document.DocumentLevel IS 'Depth in the document hierarchy.';
  COMMENT ON COLUMN Production.Document.Title IS 'Title of the document.';
  COMMENT ON COLUMN Production.Document.Owner IS 'Employee who controls the document.  Foreign key to Employee.BusinessEntityID';
  COMMENT ON COLUMN Production.Document.FolderFlag IS '0 = This is a folder, 1 = This is a document.';
  COMMENT ON COLUMN Production.Document.FileName IS 'File name of the document';
  COMMENT ON COLUMN Production.Document.FileExtension IS 'File extension indicating the document type. For example, .doc or .txt.';
  COMMENT ON COLUMN Production.Document.Revision IS 'Revision number of the document.';
  COMMENT ON COLUMN Production.Document.ChangeNumber IS 'Engineering change approval number.';
  COMMENT ON COLUMN Production.Document.Status IS '1 = Pending approval, 2 = Approved, 3 = Obsolete';
  COMMENT ON COLUMN Production.Document.DocumentSummary IS 'Document abstract.';
  COMMENT ON COLUMN Production.Document.Document IS 'Complete document.';
  COMMENT ON COLUMN Production.Document.rowguid IS 'ROWGUIDCOL number uniquely identifying the record. Required for FileStream.';

COMMENT ON TABLE Person.EmailAddress IS 'Where to send a person email.';
  COMMENT ON COLUMN Person.EmailAddress.BusinessEntityID IS 'Primary key. Person associated with this email address.  Foreign key to Person.BusinessEntityID';
  COMMENT ON COLUMN Person.EmailAddress.EmailAddressID IS 'Primary key. ID of this email address.';
  COMMENT ON COLUMN Person.EmailAddress.EmailAddress IS 'E-mail address for the person.';

COMMENT ON TABLE HumanResources.Employee IS 'Employee information such as salary, department, and title.';
  COMMENT ON COLUMN HumanResources.Employee.BusinessEntityID IS 'Primary key for Employee records.  Foreign key to BusinessEntity.BusinessEntityID.';
  COMMENT ON COLUMN HumanResources.Employee.NationalIDNumber IS 'Unique national identification number such as a social security number.';
  COMMENT ON COLUMN HumanResources.Employee.LoginID IS 'Network login.';
  COMMENT ON COLUMN HumanResources.Employee.OrganizationNode IS 'Where the employee is located in corporate hierarchy.';
--  COMMENT ON COLUMN HumanResources.Employee.OrganizationLevel IS 'The depth of the employee in the corporate hierarchy.';
  COMMENT ON COLUMN HumanResources.Employee.JobTitle IS 'Work title such as Buyer or Sales Representative.';
  COMMENT ON COLUMN HumanResources.Employee.BirthDate IS 'Date of birth.';
  COMMENT ON COLUMN HumanResources.Employee.MaritalStatus IS 'M = Married, S = Single';
  COMMENT ON COLUMN HumanResources.Employee.Gender IS 'M = Male, F = Female';
  COMMENT ON COLUMN HumanResources.Employee.HireDate IS 'Employee hired on this date.';
  COMMENT ON COLUMN HumanResources.Employee.SalariedFlag IS 'Job classification. 0 = Hourly, not exempt from collective bargaining. 1 = Salaried, exempt from collective bargaining.';
  COMMENT ON COLUMN HumanResources.Employee.VacationHours IS 'Number of available vacation hours.';
  COMMENT ON COLUMN HumanResources.Employee.SickLeaveHours IS 'Number of available sick leave hours.';
  COMMENT ON COLUMN HumanResources.Employee.CurrentFlag IS '0 = Inactive, 1 = Active';

COMMENT ON TABLE HumanResources.EmployeeDepartmentHistory IS 'Employee department transfers.';
  COMMENT ON COLUMN HumanResources.EmployeeDepartmentHistory.BusinessEntityID IS 'Employee identification number. Foreign key to Employee.BusinessEntityID.';
  COMMENT ON COLUMN HumanResources.EmployeeDepartmentHistory.DepartmentID IS 'Department in which the employee worked including currently. Foreign key to Department.DepartmentID.';
  COMMENT ON COLUMN HumanResources.EmployeeDepartmentHistory.ShiftID IS 'Identifies which 8-hour shift the employee works. Foreign key to Shift.Shift.ID.';
  COMMENT ON COLUMN HumanResources.EmployeeDepartmentHistory.StartDate IS 'Date the employee started work in the department.';
  COMMENT ON COLUMN HumanResources.EmployeeDepartmentHistory.EndDate IS 'Date the employee left the department. NULL = Current department.';

COMMENT ON TABLE HumanResources.EmployeePayHistory IS 'Employee pay history.';
  COMMENT ON COLUMN HumanResources.EmployeePayHistory.BusinessEntityID IS 'Employee identification number. Foreign key to Employee.BusinessEntityID.';
  COMMENT ON COLUMN HumanResources.EmployeePayHistory.RateChangeDate IS 'Date the change in pay is effective';
  COMMENT ON COLUMN HumanResources.EmployeePayHistory.Rate IS 'Salary hourly rate.';
  COMMENT ON COLUMN HumanResources.EmployeePayHistory.PayFrequency IS '1 = Salary received monthly, 2 = Salary received biweekly';

COMMENT ON TABLE Production.Illustration IS 'Bicycle assembly diagrams.';
  COMMENT ON COLUMN Production.Illustration.IllustrationID IS 'Primary key for Illustration records.';
  COMMENT ON COLUMN Production.Illustration.Diagram IS 'Illustrations used in manufacturing instructions. Stored as XML.';

COMMENT ON TABLE HumanResources.JobCandidate IS 'Résumés submitted to Human Resources by job applicants.';
  COMMENT ON COLUMN HumanResources.JobCandidate.JobCandidateID IS 'Primary key for JobCandidate records.';
  COMMENT ON COLUMN HumanResources.JobCandidate.BusinessEntityID IS 'Employee identification number if applicant was hired. Foreign key to Employee.BusinessEntityID.';
  COMMENT ON COLUMN HumanResources.JobCandidate.Resume IS 'Résumé in XML format.';

COMMENT ON TABLE Production.Location IS 'Product inventory and manufacturing locations.';
  COMMENT ON COLUMN Production.Location.LocationID IS 'Primary key for Location records.';
  COMMENT ON COLUMN Production.Location.Name IS 'Location description.';
  COMMENT ON COLUMN Production.Location.CostRate IS 'Standard hourly cost of the manufacturing location.';
  COMMENT ON COLUMN Production.Location.Availability IS 'Work capacity (in hours) of the manufacturing location.';

COMMENT ON TABLE Person.Password IS 'One way hashed authentication information';
  COMMENT ON COLUMN Person.Password.PasswordHash IS 'Password for the e-mail account.';
  COMMENT ON COLUMN Person.Password.PasswordSalt IS 'Random value concatenated with the password string before the password is hashed.';

COMMENT ON TABLE Person.Person IS 'Human beings involved with AdventureWorks: employees, customer contacts, and vendor contacts.';
  COMMENT ON COLUMN Person.Person.BusinessEntityID IS 'Primary key for Person records.';
  COMMENT ON COLUMN Person.Person.PersonType IS 'Primary type of person: SC = Store Contact, IN = Individual (retail) customer, SP = Sales person, EM = Employee (non-sales), VC = Vendor contact, GC = General contact';
  COMMENT ON COLUMN Person.Person.NameStyle IS '0 = The data in FirstName and LastName are stored in western style (first name, last name) order.  1 = Eastern style (last name, first name) order.';
  COMMENT ON COLUMN Person.Person.Title IS 'A courtesy title. For example, Mr. or Ms.';
  COMMENT ON COLUMN Person.Person.FirstName IS 'First name of the person.';
  COMMENT ON COLUMN Person.Person.MiddleName IS 'Middle name or middle initial of the person.';
  COMMENT ON COLUMN Person.Person.LastName IS 'Last name of the person.';
  COMMENT ON COLUMN Person.Person.Suffix IS 'Surname suffix. For example, Sr. or Jr.';
  COMMENT ON COLUMN Person.Person.EmailPromotion IS '0 = Contact does not wish to receive e-mail promotions, 1 = Contact does wish to receive e-mail promotions from AdventureWorks, 2 = Contact does wish to receive e-mail promotions from AdventureWorks and selected partners.';
  COMMENT ON COLUMN Person.Person.Demographics IS 'Personal information such as hobbies, and income collected from online shoppers. Used for sales analysis.';
  COMMENT ON COLUMN Person.Person.AdditionalContactInfo IS 'Additional contact information about the person stored in xml format.';

COMMENT ON TABLE Sales.PersonCreditCard IS 'Cross-reference table mapping people to their credit card information in the CreditCard table.';
  COMMENT ON COLUMN Sales.PersonCreditCard.BusinessEntityID IS 'Business entity identification number. Foreign key to Person.BusinessEntityID.';
  COMMENT ON COLUMN Sales.PersonCreditCard.CreditCardID IS 'Credit card identification number. Foreign key to CreditCard.CreditCardID.';

COMMENT ON TABLE Person.PersonPhone IS 'Telephone number and type of a person.';
  COMMENT ON COLUMN Person.PersonPhone.BusinessEntityID IS 'Business entity identification number. Foreign key to Person.BusinessEntityID.';
  COMMENT ON COLUMN Person.PersonPhone.PhoneNumber IS 'Telephone number identification number.';
  COMMENT ON COLUMN Person.PersonPhone.PhoneNumberTypeID IS 'Kind of phone number. Foreign key to PhoneNumberType.PhoneNumberTypeID.';

COMMENT ON TABLE Person.PhoneNumberType IS 'Type of phone number of a person.';
  COMMENT ON COLUMN Person.PhoneNumberType.PhoneNumberTypeID IS 'Primary key for telephone number type records.';
  COMMENT ON COLUMN Person.PhoneNumberType.Name IS 'Name of the telephone number type';

COMMENT ON TABLE Production.Product IS 'Products sold or used in the manfacturing of sold products.';
  COMMENT ON COLUMN Production.Product.ProductID IS 'Primary key for Product records.';
  COMMENT ON COLUMN Production.Product.Name IS 'Name of the product.';
  COMMENT ON COLUMN Production.Product.ProductNumber IS 'Unique product identification number.';
  COMMENT ON COLUMN Production.Product.MakeFlag IS '0 = Product is purchased, 1 = Product is manufactured in-house.';
  COMMENT ON COLUMN Production.Product.FinishedGoodsFlag IS '0 = Product is not a salable item. 1 = Product is salable.';
  COMMENT ON COLUMN Production.Product.Color IS 'Product color.';
  COMMENT ON COLUMN Production.Product.SafetyStockLevel IS 'Minimum inventory quantity.';
  COMMENT ON COLUMN Production.Product.ReorderPoint IS 'Inventory level that triggers a purchase order or work order.';
  COMMENT ON COLUMN Production.Product.StandardCost IS 'Standard cost of the product.';
  COMMENT ON COLUMN Production.Product.ListPrice IS 'Selling price.';
  COMMENT ON COLUMN Production.Product.Size IS 'Product size.';
  COMMENT ON COLUMN Production.Product.SizeUnitMeasureCode IS 'Unit of measure for Size column.';
  COMMENT ON COLUMN Production.Product.WeightUnitMeasureCode IS 'Unit of measure for Weight column.';
  COMMENT ON COLUMN Production.Product.Weight IS 'Product weight.';
  COMMENT ON COLUMN Production.Product.DaysToManufacture IS 'Number of days required to manufacture the product.';
  COMMENT ON COLUMN Production.Product.ProductLine IS 'R = Road, M = Mountain, T = Touring, S = Standard';
  COMMENT ON COLUMN Production.Product.Class IS 'H = High, M = Medium, L = Low';
  COMMENT ON COLUMN Production.Product.Style IS 'W = Womens, M = Mens, U = Universal';
  COMMENT ON COLUMN Production.Product.ProductSubcategoryID IS 'Product is a member of this product subcategory. Foreign key to ProductSubCategory.ProductSubCategoryID.';
  COMMENT ON COLUMN Production.Product.ProductModelID IS 'Product is a member of this product model. Foreign key to ProductModel.ProductModelID.';
  COMMENT ON COLUMN Production.Product.SellStartDate IS 'Date the product was available for sale.';
  COMMENT ON COLUMN Production.Product.SellEndDate IS 'Date the product was no longer available for sale.';
  COMMENT ON COLUMN Production.Product.DiscontinuedDate IS 'Date the product was discontinued.';

COMMENT ON TABLE Production.ProductCategory IS 'High-level product categorization.';
  COMMENT ON COLUMN Production.ProductCategory.ProductCategoryID IS 'Primary key for ProductCategory records.';
  COMMENT ON COLUMN Production.ProductCategory.Name IS 'Category description.';

COMMENT ON TABLE Production.ProductCostHistory IS 'Changes in the cost of a product over time.';
  COMMENT ON COLUMN Production.ProductCostHistory.ProductID IS 'Product identification number. Foreign key to Product.ProductID';
  COMMENT ON COLUMN Production.ProductCostHistory.StartDate IS 'Product cost start date.';
  COMMENT ON COLUMN Production.ProductCostHistory.EndDate IS 'Product cost end date.';
  COMMENT ON COLUMN Production.ProductCostHistory.StandardCost IS 'Standard cost of the product.';

COMMENT ON TABLE Production.ProductDescription IS 'Product descriptions in several languages.';
  COMMENT ON COLUMN Production.ProductDescription.ProductDescriptionID IS 'Primary key for ProductDescription records.';
  COMMENT ON COLUMN Production.ProductDescription.Description IS 'Description of the product.';

COMMENT ON TABLE Production.ProductDocument IS 'Cross-reference table mapping products to related product documents.';
  COMMENT ON COLUMN Production.ProductDocument.ProductID IS 'Product identification number. Foreign key to Product.ProductID.';
  COMMENT ON COLUMN Production.ProductDocument.DocumentNode IS 'Document identification number. Foreign key to Document.DocumentNode.';

COMMENT ON TABLE Production.ProductInventory IS 'Product inventory information.';
  COMMENT ON COLUMN Production.ProductInventory.ProductID IS 'Product identification number. Foreign key to Product.ProductID.';
  COMMENT ON COLUMN Production.ProductInventory.LocationID IS 'Inventory location identification number. Foreign key to Location.LocationID.';
  COMMENT ON COLUMN Production.ProductInventory.Shelf IS 'Storage compartment within an inventory location.';
  COMMENT ON COLUMN Production.ProductInventory.Bin IS 'Storage container on a shelf in an inventory location.';
  COMMENT ON COLUMN Production.ProductInventory.Quantity IS 'Quantity of products in the inventory location.';

COMMENT ON TABLE Production.ProductListPriceHistory IS 'Changes in the list price of a product over time.';
  COMMENT ON COLUMN Production.ProductListPriceHistory.ProductID IS 'Product identification number. Foreign key to Product.ProductID';
  COMMENT ON COLUMN Production.ProductListPriceHistory.StartDate IS 'List price start date.';
  COMMENT ON COLUMN Production.ProductListPriceHistory.EndDate IS 'List price end date';
  COMMENT ON COLUMN Production.ProductListPriceHistory.ListPrice IS 'Product list price.';

COMMENT ON TABLE Production.ProductModel IS 'Product model classification.';
  COMMENT ON COLUMN Production.ProductModel.ProductModelID IS 'Primary key for ProductModel records.';
  COMMENT ON COLUMN Production.ProductModel.Name IS 'Product model description.';
  COMMENT ON COLUMN Production.ProductModel.CatalogDescription IS 'Detailed product catalog information in xml format.';
  COMMENT ON COLUMN Production.ProductModel.Instructions IS 'Manufacturing instructions in xml format.';

COMMENT ON TABLE Production.ProductModelIllustration IS 'Cross-reference table mapping product models and illustrations.';
  COMMENT ON COLUMN Production.ProductModelIllustration.ProductModelID IS 'Primary key. Foreign key to ProductModel.ProductModelID.';
  COMMENT ON COLUMN Production.ProductModelIllustration.IllustrationID IS 'Primary key. Foreign key to Illustration.IllustrationID.';

COMMENT ON TABLE Production.ProductModelProductDescriptionCulture IS 'Cross-reference table mapping product descriptions and the language the description is written in.';
  COMMENT ON COLUMN Production.ProductModelProductDescriptionCulture.ProductModelID IS 'Primary key. Foreign key to ProductModel.ProductModelID.';
  COMMENT ON COLUMN Production.ProductModelProductDescriptionCulture.ProductDescriptionID IS 'Primary key. Foreign key to ProductDescription.ProductDescriptionID.';
  COMMENT ON COLUMN Production.ProductModelProductDescriptionCulture.CultureID IS 'Culture identification number. Foreign key to Culture.CultureID.';

COMMENT ON TABLE Production.ProductPhoto IS 'Product images.';
  COMMENT ON COLUMN Production.ProductPhoto.ProductPhotoID IS 'Primary key for ProductPhoto records.';
  COMMENT ON COLUMN Production.ProductPhoto.ThumbNailPhoto IS 'Small image of the product.';
  COMMENT ON COLUMN Production.ProductPhoto.ThumbnailPhotoFileName IS 'Small image file name.';
  COMMENT ON COLUMN Production.ProductPhoto.LargePhoto IS 'Large image of the product.';
  COMMENT ON COLUMN Production.ProductPhoto.LargePhotoFileName IS 'Large image file name.';

COMMENT ON TABLE Production.ProductProductPhoto IS 'Cross-reference table mapping products and product photos.';
  COMMENT ON COLUMN Production.ProductProductPhoto.ProductID IS 'Product identification number. Foreign key to Product.ProductID.';
  COMMENT ON COLUMN Production.ProductProductPhoto.ProductPhotoID IS 'Product photo identification number. Foreign key to ProductPhoto.ProductPhotoID.';
  COMMENT ON COLUMN Production.ProductProductPhoto.Primary IS '0 = Photo is not the principal image. 1 = Photo is the principal image.';

COMMENT ON TABLE Production.ProductReview IS 'Customer reviews of products they have purchased.';
  COMMENT ON COLUMN Production.ProductReview.ProductReviewID IS 'Primary key for ProductReview records.';
  COMMENT ON COLUMN Production.ProductReview.ProductID IS 'Product identification number. Foreign key to Product.ProductID.';
  COMMENT ON COLUMN Production.ProductReview.ReviewerName IS 'Name of the reviewer.';
  COMMENT ON COLUMN Production.ProductReview.ReviewDate IS 'Date review was submitted.';
  COMMENT ON COLUMN Production.ProductReview.EmailAddress IS 'Reviewer''s e-mail address.';
  COMMENT ON COLUMN Production.ProductReview.Rating IS 'Product rating given by the reviewer. Scale is 1 to 5 with 5 as the highest rating.';
  COMMENT ON COLUMN Production.ProductReview.Comments IS 'Reviewer''s comments';

COMMENT ON TABLE Production.ProductSubcategory IS 'Product subcategories. See ProductCategory table.';
  COMMENT ON COLUMN Production.ProductSubcategory.ProductSubcategoryID IS 'Primary key for ProductSubcategory records.';
  COMMENT ON COLUMN Production.ProductSubcategory.ProductCategoryID IS 'Product category identification number. Foreign key to ProductCategory.ProductCategoryID.';
  COMMENT ON COLUMN Production.ProductSubcategory.Name IS 'Subcategory description.';

COMMENT ON TABLE Purchasing.ProductVendor IS 'Cross-reference table mapping vendors with the products they supply.';
  COMMENT ON COLUMN Purchasing.ProductVendor.ProductID IS 'Primary key. Foreign key to Product.ProductID.';
  COMMENT ON COLUMN Purchasing.ProductVendor.BusinessEntityID IS 'Primary key. Foreign key to Vendor.BusinessEntityID.';
  COMMENT ON COLUMN Purchasing.ProductVendor.AverageLeadTime IS 'The average span of time (in days) between placing an order with the vendor and receiving the purchased product.';
  COMMENT ON COLUMN Purchasing.ProductVendor.StandardPrice IS 'The vendor''s usual selling price.';
  COMMENT ON COLUMN Purchasing.ProductVendor.LastReceiptCost IS 'The selling price when last purchased.';
  COMMENT ON COLUMN Purchasing.ProductVendor.LastReceiptDate IS 'Date the product was last received by the vendor.';
  COMMENT ON COLUMN Purchasing.ProductVendor.MinOrderQty IS 'The maximum quantity that should be ordered.';
  COMMENT ON COLUMN Purchasing.ProductVendor.MaxOrderQty IS 'The minimum quantity that should be ordered.';
  COMMENT ON COLUMN Purchasing.ProductVendor.OnOrderQty IS 'The quantity currently on order.';
  COMMENT ON COLUMN Purchasing.ProductVendor.UnitMeasureCode IS 'The product''s unit of measure.';

COMMENT ON TABLE Purchasing.PurchaseOrderDetail IS 'Individual products associated with a specific purchase order. See PurchaseOrderHeader.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderDetail.PurchaseOrderID IS 'Primary key. Foreign key to PurchaseOrderHeader.PurchaseOrderID.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderDetail.PurchaseOrderDetailID IS 'Primary key. One line number per purchased product.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderDetail.DueDate IS 'Date the product is expected to be received.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderDetail.OrderQty IS 'Quantity ordered.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderDetail.ProductID IS 'Product identification number. Foreign key to Product.ProductID.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderDetail.UnitPrice IS 'Vendor''s selling price of a single product.';
--  COMMENT ON COLUMN Purchasing.PurchaseOrderDetail.LineTotal IS 'Per product subtotal. Computed as OrderQty * UnitPrice.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderDetail.ReceivedQty IS 'Quantity actually received from the vendor.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderDetail.RejectedQty IS 'Quantity rejected during inspection.';
--  COMMENT ON COLUMN Purchasing.PurchaseOrderDetail.StockedQty IS 'Quantity accepted into inventory. Computed as ReceivedQty - RejectedQty.';

COMMENT ON TABLE Purchasing.PurchaseOrderHeader IS 'General purchase order information. See PurchaseOrderDetail.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderHeader.PurchaseOrderID IS 'Primary key.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderHeader.RevisionNumber IS 'Incremental number to track changes to the purchase order over time.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderHeader.Status IS 'Order current status. 1 = Pending; 2 = Approved; 3 = Rejected; 4 = Complete';
  COMMENT ON COLUMN Purchasing.PurchaseOrderHeader.EmployeeID IS 'Employee who created the purchase order. Foreign key to Employee.BusinessEntityID.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderHeader.VendorID IS 'Vendor with whom the purchase order is placed. Foreign key to Vendor.BusinessEntityID.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderHeader.ShipMethodID IS 'Shipping method. Foreign key to ShipMethod.ShipMethodID.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderHeader.OrderDate IS 'Purchase order creation date.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderHeader.ShipDate IS 'Estimated shipment date from the vendor.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderHeader.SubTotal IS 'Purchase order subtotal. Computed as SUM(PurchaseOrderDetail.LineTotal)for the appropriate PurchaseOrderID.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderHeader.TaxAmt IS 'Tax amount.';
  COMMENT ON COLUMN Purchasing.PurchaseOrderHeader.Freight IS 'Shipping cost.';
--  COMMENT ON COLUMN Purchasing.PurchaseOrderHeader.TotalDue IS 'Total due to vendor. Computed as Subtotal + TaxAmt + Freight.';

COMMENT ON TABLE Sales.SalesOrderDetail IS 'Individual products associated with a specific sales order. See SalesOrderHeader.';
  COMMENT ON COLUMN Sales.SalesOrderDetail.SalesOrderID IS 'Primary key. Foreign key to SalesOrderHeader.SalesOrderID.';
  COMMENT ON COLUMN Sales.SalesOrderDetail.SalesOrderDetailID IS 'Primary key. One incremental unique number per product sold.';
  COMMENT ON COLUMN Sales.SalesOrderDetail.CarrierTrackingNumber IS 'Shipment tracking number supplied by the shipper.';
  COMMENT ON COLUMN Sales.SalesOrderDetail.OrderQty IS 'Quantity ordered per product.';
  COMMENT ON COLUMN Sales.SalesOrderDetail.ProductID IS 'Product sold to customer. Foreign key to Product.ProductID.';
  COMMENT ON COLUMN Sales.SalesOrderDetail.SpecialOfferID IS 'Promotional code. Foreign key to SpecialOffer.SpecialOfferID.';
  COMMENT ON COLUMN Sales.SalesOrderDetail.UnitPrice IS 'Selling price of a single product.';
  COMMENT ON COLUMN Sales.SalesOrderDetail.UnitPriceDiscount IS 'Discount amount.';
--  COMMENT ON COLUMN Sales.SalesOrderDetail.LineTotal IS 'Per product subtotal. Computed as UnitPrice * (1 - UnitPriceDiscount) * OrderQty.';

COMMENT ON TABLE Sales.SalesOrderHeader IS 'General sales order information.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.SalesOrderID IS 'Primary key.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.RevisionNumber IS 'Incremental number to track changes to the sales order over time.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.OrderDate IS 'Dates the sales order was created.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.DueDate IS 'Date the order is due to the customer.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.ShipDate IS 'Date the order was shipped to the customer.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.Status IS 'Order current status. 1 = In process; 2 = Approved; 3 = Backordered; 4 = Rejected; 5 = Shipped; 6 = Cancelled';
  COMMENT ON COLUMN Sales.SalesOrderHeader.OnlineOrderFlag IS '0 = Order placed by sales person. 1 = Order placed online by customer.';
--  COMMENT ON COLUMN Sales.SalesOrderHeader.SalesOrderNumber IS 'Unique sales order identification number.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.PurchaseOrderNumber IS 'Customer purchase order number reference.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.AccountNumber IS 'Financial accounting number reference.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.CustomerID IS 'Customer identification number. Foreign key to Customer.BusinessEntityID.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.SalesPersonID IS 'Sales person who created the sales order. Foreign key to SalesPerson.BusinessEntityID.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.TerritoryID IS 'Territory in which the sale was made. Foreign key to SalesTerritory.SalesTerritoryID.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.BillToAddressID IS 'Customer billing address. Foreign key to Address.AddressID.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.ShipToAddressID IS 'Customer shipping address. Foreign key to Address.AddressID.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.ShipMethodID IS 'Shipping method. Foreign key to ShipMethod.ShipMethodID.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.CreditCardID IS 'Credit card identification number. Foreign key to CreditCard.CreditCardID.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.CreditCardApprovalCode IS 'Approval code provided by the credit card company.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.CurrencyRateID IS 'Currency exchange rate used. Foreign key to CurrencyRate.CurrencyRateID.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.SubTotal IS 'Sales subtotal. Computed as SUM(SalesOrderDetail.LineTotal)for the appropriate SalesOrderID.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.TaxAmt IS 'Tax amount.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.Freight IS 'Shipping cost.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.TotalDue IS 'Total due from customer. Computed as Subtotal + TaxAmt + Freight.';
  COMMENT ON COLUMN Sales.SalesOrderHeader.Comment IS 'Sales representative comments.';

COMMENT ON TABLE Sales.SalesOrderHeaderSalesReason IS 'Cross-reference table mapping sales orders to sales reason codes.';
  COMMENT ON COLUMN Sales.SalesOrderHeaderSalesReason.SalesOrderID IS 'Primary key. Foreign key to SalesOrderHeader.SalesOrderID.';
  COMMENT ON COLUMN Sales.SalesOrderHeaderSalesReason.SalesReasonID IS 'Primary key. Foreign key to SalesReason.SalesReasonID.';

COMMENT ON TABLE Sales.SalesPerson IS 'Sales representative current information.';
  COMMENT ON COLUMN Sales.SalesPerson.BusinessEntityID IS 'Primary key for SalesPerson records. Foreign key to Employee.BusinessEntityID';
  COMMENT ON COLUMN Sales.SalesPerson.TerritoryID IS 'Territory currently assigned to. Foreign key to SalesTerritory.SalesTerritoryID.';
  COMMENT ON COLUMN Sales.SalesPerson.SalesQuota IS 'Projected yearly sales.';
  COMMENT ON COLUMN Sales.SalesPerson.Bonus IS 'Bonus due if quota is met.';
  COMMENT ON COLUMN Sales.SalesPerson.CommissionPct IS 'Commision percent received per sale.';
  COMMENT ON COLUMN Sales.SalesPerson.SalesYTD IS 'Sales total year to date.';
  COMMENT ON COLUMN Sales.SalesPerson.SalesLastYear IS 'Sales total of previous year.';

COMMENT ON TABLE Sales.SalesPersonQuotaHistory IS 'Sales performance tracking.';
  COMMENT ON COLUMN Sales.SalesPersonQuotaHistory.BusinessEntityID IS 'Sales person identification number. Foreign key to SalesPerson.BusinessEntityID.';
  COMMENT ON COLUMN Sales.SalesPersonQuotaHistory.QuotaDate IS 'Sales quota date.';
  COMMENT ON COLUMN Sales.SalesPersonQuotaHistory.SalesQuota IS 'Sales quota amount.';

COMMENT ON TABLE Sales.SalesReason IS 'Lookup table of customer purchase reasons.';
  COMMENT ON COLUMN Sales.SalesReason.SalesReasonID IS 'Primary key for SalesReason records.';
  COMMENT ON COLUMN Sales.SalesReason.Name IS 'Sales reason description.';
  COMMENT ON COLUMN Sales.SalesReason.ReasonType IS 'Category the sales reason belongs to.';

COMMENT ON TABLE Sales.SalesTaxRate IS 'Tax rate lookup table.';
  COMMENT ON COLUMN Sales.SalesTaxRate.SalesTaxRateID IS 'Primary key for SalesTaxRate records.';
  COMMENT ON COLUMN Sales.SalesTaxRate.StateProvinceID IS 'State, province, or country/region the sales tax applies to.';
  COMMENT ON COLUMN Sales.SalesTaxRate.TaxType IS '1 = Tax applied to retail transactions, 2 = Tax applied to wholesale transactions, 3 = Tax applied to all sales (retail and wholesale) transactions.';
  COMMENT ON COLUMN Sales.SalesTaxRate.TaxRate IS 'Tax rate amount.';
  COMMENT ON COLUMN Sales.SalesTaxRate.Name IS 'Tax rate description.';

COMMENT ON TABLE Sales.SalesTerritory IS 'Sales territory lookup table.';
  COMMENT ON COLUMN Sales.SalesTerritory.TerritoryID IS 'Primary key for SalesTerritory records.';
  COMMENT ON COLUMN Sales.SalesTerritory.Name IS 'Sales territory description';
  COMMENT ON COLUMN Sales.SalesTerritory.CountryRegionCode IS 'ISO standard country or region code. Foreign key to CountryRegion.CountryRegionCode.';
  COMMENT ON COLUMN Sales.SalesTerritory.Group IS 'Geographic area to which the sales territory belong.';
  COMMENT ON COLUMN Sales.SalesTerritory.SalesYTD IS 'Sales in the territory year to date.';
  COMMENT ON COLUMN Sales.SalesTerritory.SalesLastYear IS 'Sales in the territory the previous year.';
  COMMENT ON COLUMN Sales.SalesTerritory.CostYTD IS 'Business costs in the territory year to date.';
  COMMENT ON COLUMN Sales.SalesTerritory.CostLastYear IS 'Business costs in the territory the previous year.';

COMMENT ON TABLE Sales.SalesTerritoryHistory IS 'Sales representative transfers to other sales territories.';
  COMMENT ON COLUMN Sales.SalesTerritoryHistory.BusinessEntityID IS 'Primary key. The sales rep.  Foreign key to SalesPerson.BusinessEntityID.';
  COMMENT ON COLUMN Sales.SalesTerritoryHistory.TerritoryID IS 'Primary key. Territory identification number. Foreign key to SalesTerritory.SalesTerritoryID.';
  COMMENT ON COLUMN Sales.SalesTerritoryHistory.StartDate IS 'Primary key. Date the sales representive started work in the territory.';
  COMMENT ON COLUMN Sales.SalesTerritoryHistory.EndDate IS 'Date the sales representative left work in the territory.';

COMMENT ON TABLE Production.ScrapReason IS 'Manufacturing failure reasons lookup table.';
  COMMENT ON COLUMN Production.ScrapReason.ScrapReasonID IS 'Primary key for ScrapReason records.';
  COMMENT ON COLUMN Production.ScrapReason.Name IS 'Failure description.';

COMMENT ON TABLE HumanResources.Shift IS 'Work shift lookup table.';
  COMMENT ON COLUMN HumanResources.Shift.ShiftID IS 'Primary key for Shift records.';
  COMMENT ON COLUMN HumanResources.Shift.Name IS 'Shift description.';
  COMMENT ON COLUMN HumanResources.Shift.StartTime IS 'Shift start time.';
  COMMENT ON COLUMN HumanResources.Shift.EndTime IS 'Shift end time.';

COMMENT ON TABLE Purchasing.ShipMethod IS 'Shipping company lookup table.';
  COMMENT ON COLUMN Purchasing.ShipMethod.ShipMethodID IS 'Primary key for ShipMethod records.';
  COMMENT ON COLUMN Purchasing.ShipMethod.Name IS 'Shipping company name.';
  COMMENT ON COLUMN Purchasing.ShipMethod.ShipBase IS 'Minimum shipping charge.';
  COMMENT ON COLUMN Purchasing.ShipMethod.ShipRate IS 'Shipping charge per pound.';

COMMENT ON TABLE Sales.ShoppingCartItem IS 'Contains online customer orders until the order is submitted or cancelled.';
  COMMENT ON COLUMN Sales.ShoppingCartItem.ShoppingCartItemID IS 'Primary key for ShoppingCartItem records.';
  COMMENT ON COLUMN Sales.ShoppingCartItem.ShoppingCartID IS 'Shopping cart identification number.';
  COMMENT ON COLUMN Sales.ShoppingCartItem.Quantity IS 'Product quantity ordered.';
  COMMENT ON COLUMN Sales.ShoppingCartItem.ProductID IS 'Product ordered. Foreign key to Product.ProductID.';
  COMMENT ON COLUMN Sales.ShoppingCartItem.DateCreated IS 'Date the time the record was created.';

COMMENT ON TABLE Sales.SpecialOffer IS 'Sale discounts lookup table.';
  COMMENT ON COLUMN Sales.SpecialOffer.SpecialOfferID IS 'Primary key for SpecialOffer records.';
  COMMENT ON COLUMN Sales.SpecialOffer.Description IS 'Discount description.';
  COMMENT ON COLUMN Sales.SpecialOffer.DiscountPct IS 'Discount precentage.';
  COMMENT ON COLUMN Sales.SpecialOffer.Type IS 'Discount type category.';
  COMMENT ON COLUMN Sales.SpecialOffer.Category IS 'Group the discount applies to such as Reseller or Customer.';
  COMMENT ON COLUMN Sales.SpecialOffer.StartDate IS 'Discount start date.';
  COMMENT ON COLUMN Sales.SpecialOffer.EndDate IS 'Discount end date.';
  COMMENT ON COLUMN Sales.SpecialOffer.MinQty IS 'Minimum discount percent allowed.';
  COMMENT ON COLUMN Sales.SpecialOffer.MaxQty IS 'Maximum discount percent allowed.';

COMMENT ON TABLE Sales.SpecialOfferProduct IS 'Cross-reference table mapping products to special offer discounts.';
  COMMENT ON COLUMN Sales.SpecialOfferProduct.SpecialOfferID IS 'Primary key for SpecialOfferProduct records.';
  COMMENT ON COLUMN Sales.SpecialOfferProduct.ProductID IS 'Product identification number. Foreign key to Product.ProductID.';

COMMENT ON TABLE Person.StateProvince IS 'State and province lookup table.';
  COMMENT ON COLUMN Person.StateProvince.StateProvinceID IS 'Primary key for StateProvince records.';
  COMMENT ON COLUMN Person.StateProvince.StateProvinceCode IS 'ISO standard state or province code.';
  COMMENT ON COLUMN Person.StateProvince.CountryRegionCode IS 'ISO standard country or region code. Foreign key to CountryRegion.CountryRegionCode.';
  COMMENT ON COLUMN Person.StateProvince.IsOnlyStateProvinceFlag IS '0 = StateProvinceCode exists. 1 = StateProvinceCode unavailable, using CountryRegionCode.';
  COMMENT ON COLUMN Person.StateProvince.Name IS 'State or province description.';
  COMMENT ON COLUMN Person.StateProvince.TerritoryID IS 'ID of the territory in which the state or province is located. Foreign key to SalesTerritory.SalesTerritoryID.';

COMMENT ON TABLE Sales.Store IS 'Customers (resellers) of Adventure Works products.';
  COMMENT ON COLUMN Sales.Store.BusinessEntityID IS 'Primary key. Foreign key to Customer.BusinessEntityID.';
  COMMENT ON COLUMN Sales.Store.Name IS 'Name of the store.';
  COMMENT ON COLUMN Sales.Store.SalesPersonID IS 'ID of the sales person assigned to the customer. Foreign key to SalesPerson.BusinessEntityID.';
  COMMENT ON COLUMN Sales.Store.Demographics IS 'Demographic informationg about the store such as the number of employees, annual sales and store type.';


COMMENT ON TABLE Production.TransactionHistory IS 'Record of each purchase order, sales order, or work order transaction year to date.';
  COMMENT ON COLUMN Production.TransactionHistory.TransactionID IS 'Primary key for TransactionHistory records.';
  COMMENT ON COLUMN Production.TransactionHistory.ProductID IS 'Product identification number. Foreign key to Product.ProductID.';
  COMMENT ON COLUMN Production.TransactionHistory.ReferenceOrderID IS 'Purchase order, sales order, or work order identification number.';
  COMMENT ON COLUMN Production.TransactionHistory.ReferenceOrderLineID IS 'Line number associated with the purchase order, sales order, or work order.';
  COMMENT ON COLUMN Production.TransactionHistory.TransactionDate IS 'Date and time of the transaction.';
  COMMENT ON COLUMN Production.TransactionHistory.TransactionType IS 'W = WorkOrder, S = SalesOrder, P = PurchaseOrder';
  COMMENT ON COLUMN Production.TransactionHistory.Quantity IS 'Product quantity.';
  COMMENT ON COLUMN Production.TransactionHistory.ActualCost IS 'Product cost.';

COMMENT ON TABLE Production.TransactionHistoryArchive IS 'Transactions for previous years.';
  COMMENT ON COLUMN Production.TransactionHistoryArchive.TransactionID IS 'Primary key for TransactionHistoryArchive records.';
  COMMENT ON COLUMN Production.TransactionHistoryArchive.ProductID IS 'Product identification number. Foreign key to Product.ProductID.';
  COMMENT ON COLUMN Production.TransactionHistoryArchive.ReferenceOrderID IS 'Purchase order, sales order, or work order identification number.';
  COMMENT ON COLUMN Production.TransactionHistoryArchive.ReferenceOrderLineID IS 'Line number associated with the purchase order, sales order, or work order.';
  COMMENT ON COLUMN Production.TransactionHistoryArchive.TransactionDate IS 'Date and time of the transaction.';
  COMMENT ON COLUMN Production.TransactionHistoryArchive.TransactionType IS 'W = Work Order, S = Sales Order, P = Purchase Order';
  COMMENT ON COLUMN Production.TransactionHistoryArchive.Quantity IS 'Product quantity.';
  COMMENT ON COLUMN Production.TransactionHistoryArchive.ActualCost IS 'Product cost.';

COMMENT ON TABLE Production.UnitMeasure IS 'Unit of measure lookup table.';
  COMMENT ON COLUMN Production.UnitMeasure.UnitMeasureCode IS 'Primary key.';
  COMMENT ON COLUMN Production.UnitMeasure.Name IS 'Unit of measure description.';

COMMENT ON TABLE Purchasing.Vendor IS 'Companies from whom Adventure Works Cycles purchases parts or other goods.';
  COMMENT ON COLUMN Purchasing.Vendor.BusinessEntityID IS 'Primary key for Vendor records.  Foreign key to BusinessEntity.BusinessEntityID';
  COMMENT ON COLUMN Purchasing.Vendor.AccountNumber IS 'Vendor account (identification) number.';
  COMMENT ON COLUMN Purchasing.Vendor.Name IS 'Company name.';
  COMMENT ON COLUMN Purchasing.Vendor.CreditRating IS '1 = Superior, 2 = Excellent, 3 = Above average, 4 = Average, 5 = Below average';
  COMMENT ON COLUMN Purchasing.Vendor.PreferredVendorStatus IS '0 = Do not use if another vendor is available. 1 = Preferred over other vendors supplying the same product.';
  COMMENT ON COLUMN Purchasing.Vendor.ActiveFlag IS '0 = Vendor no longer used. 1 = Vendor is actively used.';
  COMMENT ON COLUMN Purchasing.Vendor.PurchasingWebServiceURL IS 'Vendor URL.';

COMMENT ON TABLE Production.WorkOrder IS 'Manufacturing work orders.';
  COMMENT ON COLUMN Production.WorkOrder.WorkOrderID IS 'Primary key for WorkOrder records.';
  COMMENT ON COLUMN Production.WorkOrder.ProductID IS 'Product identification number. Foreign key to Product.ProductID.';
  COMMENT ON COLUMN Production.WorkOrder.OrderQty IS 'Product quantity to build.';
--  COMMENT ON COLUMN Production.WorkOrder.StockedQty IS 'Quantity built and put in inventory.';
  COMMENT ON COLUMN Production.WorkOrder.ScrappedQty IS 'Quantity that failed inspection.';
  COMMENT ON COLUMN Production.WorkOrder.StartDate IS 'Work order start date.';
  COMMENT ON COLUMN Production.WorkOrder.EndDate IS 'Work order end date.';
  COMMENT ON COLUMN Production.WorkOrder.DueDate IS 'Work order due date.';
  COMMENT ON COLUMN Production.WorkOrder.ScrapReasonID IS 'Reason for inspection failure.';

COMMENT ON TABLE Production.WorkOrderRouting IS 'Work order details.';
  COMMENT ON COLUMN Production.WorkOrderRouting.WorkOrderID IS 'Primary key. Foreign key to WorkOrder.WorkOrderID.';
  COMMENT ON COLUMN Production.WorkOrderRouting.ProductID IS 'Primary key. Foreign key to Product.ProductID.';
  COMMENT ON COLUMN Production.WorkOrderRouting.OperationSequence IS 'Primary key. Indicates the manufacturing process sequence.';
  COMMENT ON COLUMN Production.WorkOrderRouting.LocationID IS 'Manufacturing location where the part is processed. Foreign key to Location.LocationID.';
  COMMENT ON COLUMN Production.WorkOrderRouting.ScheduledStartDate IS 'Planned manufacturing start date.';
  COMMENT ON COLUMN Production.WorkOrderRouting.ScheduledEndDate IS 'Planned manufacturing end date.';
  COMMENT ON COLUMN Production.WorkOrderRouting.ActualStartDate IS 'Actual start date.';
  COMMENT ON COLUMN Production.WorkOrderRouting.ActualEndDate IS 'Actual end date.';
  COMMENT ON COLUMN Production.WorkOrderRouting.ActualResourceHrs IS 'Number of manufacturing hours used.';
  COMMENT ON COLUMN Production.WorkOrderRouting.PlannedCost IS 'Estimated manufacturing cost.';
  COMMENT ON COLUMN Production.WorkOrderRouting.ActualCost IS 'Actual manufacturing cost.';



-------------------------------------
-- PRIMARY KEYS
-------------------------------------

-- ALTER TABLE dbo.AWBuildVersion ADD
--     CONSTRAINT "PK_AWBuildVersion_SystemInformationID" PRIMARY KEY
--     (SystemInformationID);
-- CLUSTER dbo.AWBuildVersion USING "PK_AWBuildVersion_SystemInformationID";

-- ALTER TABLE dbo.DatabaseLog ADD
--     CONSTRAINT "PK_DatabaseLog_DatabaseLogID" PRIMARY KEY
--     (DatabaseLogID);

ALTER TABLE Person.Address ADD
    CONSTRAINT "PK_Address_AddressID" PRIMARY KEY
    (AddressID);
CLUSTER Person.Address USING "PK_Address_AddressID";

ALTER TABLE Person.AddressType ADD
    CONSTRAINT "PK_AddressType_AddressTypeID" PRIMARY KEY
    (AddressTypeID);
CLUSTER Person.AddressType USING "PK_AddressType_AddressTypeID";

ALTER TABLE Production.BillOfMaterials ADD
    CONSTRAINT "PK_BillOfMaterials_BillOfMaterialsID" PRIMARY KEY
    (BillOfMaterialsID);

ALTER TABLE Person.BusinessEntity ADD
    CONSTRAINT "PK_BusinessEntity_BusinessEntityID" PRIMARY KEY
    (BusinessEntityID);
CLUSTER Person.BusinessEntity USING "PK_BusinessEntity_BusinessEntityID";

ALTER TABLE Person.BusinessEntityAddress ADD
    CONSTRAINT "PK_BusinessEntityAddress_BusinessEntityID_AddressID_AddressType" PRIMARY KEY
    (BusinessEntityID, AddressID, AddressTypeID);
CLUSTER Person.BusinessEntityAddress USING "PK_BusinessEntityAddress_BusinessEntityID_AddressID_AddressType";

ALTER TABLE Person.BusinessEntityContact ADD
    CONSTRAINT "PK_BusinessEntityContact_BusinessEntityID_PersonID_ContactTypeI" PRIMARY KEY
    (BusinessEntityID, PersonID, ContactTypeID);
CLUSTER Person.BusinessEntityContact USING "PK_BusinessEntityContact_BusinessEntityID_PersonID_ContactTypeI";

ALTER TABLE Person.ContactType ADD
    CONSTRAINT "PK_ContactType_ContactTypeID" PRIMARY KEY
    (ContactTypeID);
CLUSTER Person.ContactType USING "PK_ContactType_ContactTypeID";

ALTER TABLE Sales.CountryRegionCurrency ADD
    CONSTRAINT "PK_CountryRegionCurrency_CountryRegionCode_CurrencyCode" PRIMARY KEY
    (CountryRegionCode, CurrencyCode);
CLUSTER Sales.CountryRegionCurrency USING "PK_CountryRegionCurrency_CountryRegionCode_CurrencyCode";

ALTER TABLE Person.CountryRegion ADD
    CONSTRAINT "PK_CountryRegion_CountryRegionCode" PRIMARY KEY
    (CountryRegionCode);
CLUSTER Person.CountryRegion USING "PK_CountryRegion_CountryRegionCode";

ALTER TABLE Sales.CreditCard ADD
    CONSTRAINT "PK_CreditCard_CreditCardID" PRIMARY KEY
    (CreditCardID);
CLUSTER Sales.CreditCard USING "PK_CreditCard_CreditCardID";

ALTER TABLE Sales.Currency ADD
    CONSTRAINT "PK_Currency_CurrencyCode" PRIMARY KEY
    (CurrencyCode);
CLUSTER Sales.Currency USING "PK_Currency_CurrencyCode";

ALTER TABLE Sales.CurrencyRate ADD
    CONSTRAINT "PK_CurrencyRate_CurrencyRateID" PRIMARY KEY
    (CurrencyRateID);
CLUSTER Sales.CurrencyRate USING "PK_CurrencyRate_CurrencyRateID";

ALTER TABLE Sales.Customer ADD
    CONSTRAINT "PK_Customer_CustomerID" PRIMARY KEY
    (CustomerID);
CLUSTER Sales.Customer USING "PK_Customer_CustomerID";

ALTER TABLE Production.Culture ADD
    CONSTRAINT "PK_Culture_CultureID" PRIMARY KEY
    (CultureID);
CLUSTER Production.Culture USING "PK_Culture_CultureID";

ALTER TABLE Production.Document ADD
    CONSTRAINT "PK_Document_DocumentNode" PRIMARY KEY
    (DocumentNode);
CLUSTER Production.Document USING "PK_Document_DocumentNode";

ALTER TABLE Person.EmailAddress ADD
    CONSTRAINT "PK_EmailAddress_BusinessEntityID_EmailAddressID" PRIMARY KEY
    (BusinessEntityID, EmailAddressID);
CLUSTER Person.EmailAddress USING "PK_EmailAddress_BusinessEntityID_EmailAddressID";

ALTER TABLE HumanResources.Department ADD
    CONSTRAINT "PK_Department_DepartmentID" PRIMARY KEY
    (DepartmentID);
CLUSTER HumanResources.Department USING "PK_Department_DepartmentID";

ALTER TABLE HumanResources.Employee ADD
    CONSTRAINT "PK_Employee_BusinessEntityID" PRIMARY KEY
    (BusinessEntityID);
CLUSTER HumanResources.Employee USING "PK_Employee_BusinessEntityID";

ALTER TABLE HumanResources.EmployeeDepartmentHistory ADD
    CONSTRAINT "PK_EmployeeDepartmentHistory_BusinessEntityID_StartDate_Departm" PRIMARY KEY
    (BusinessEntityID, StartDate, DepartmentID, ShiftID);
CLUSTER HumanResources.EmployeeDepartmentHistory USING "PK_EmployeeDepartmentHistory_BusinessEntityID_StartDate_Departm";

ALTER TABLE HumanResources.EmployeePayHistory ADD
    CONSTRAINT "PK_EmployeePayHistory_BusinessEntityID_RateChangeDate" PRIMARY KEY
    (BusinessEntityID, RateChangeDate);
CLUSTER HumanResources.EmployeePayHistory USING "PK_EmployeePayHistory_BusinessEntityID_RateChangeDate";

ALTER TABLE HumanResources.JobCandidate ADD
    CONSTRAINT "PK_JobCandidate_JobCandidateID" PRIMARY KEY
    (JobCandidateID);
CLUSTER HumanResources.JobCandidate USING "PK_JobCandidate_JobCandidateID";

ALTER TABLE Production.Illustration ADD
    CONSTRAINT "PK_Illustration_IllustrationID" PRIMARY KEY
    (IllustrationID);
CLUSTER Production.Illustration USING "PK_Illustration_IllustrationID";

ALTER TABLE Production.Location ADD
    CONSTRAINT "PK_Location_LocationID" PRIMARY KEY
    (LocationID);
CLUSTER Production.Location USING "PK_Location_LocationID";

ALTER TABLE Person.Password ADD
    CONSTRAINT "PK_Password_BusinessEntityID" PRIMARY KEY
    (BusinessEntityID);
CLUSTER Person.Password USING "PK_Password_BusinessEntityID";

ALTER TABLE Person.Person ADD
    CONSTRAINT "PK_Person_BusinessEntityID" PRIMARY KEY
    (BusinessEntityID);
CLUSTER Person.Person USING "PK_Person_BusinessEntityID";

ALTER TABLE Person.PersonPhone ADD
    CONSTRAINT "PK_PersonPhone_BusinessEntityID_PhoneNumber_PhoneNumberTypeID" PRIMARY KEY
    (BusinessEntityID, PhoneNumber, PhoneNumberTypeID);
CLUSTER Person.PersonPhone USING "PK_PersonPhone_BusinessEntityID_PhoneNumber_PhoneNumberTypeID";

ALTER TABLE Person.PhoneNumberType ADD
    CONSTRAINT "PK_PhoneNumberType_PhoneNumberTypeID" PRIMARY KEY
    (PhoneNumberTypeID);
CLUSTER Person.PhoneNumberType USING "PK_PhoneNumberType_PhoneNumberTypeID";

ALTER TABLE Production.Product ADD
    CONSTRAINT "PK_Product_ProductID" PRIMARY KEY
    (ProductID);
CLUSTER Production.Product USING "PK_Product_ProductID";

ALTER TABLE Production.ProductCategory ADD
    CONSTRAINT "PK_ProductCategory_ProductCategoryID" PRIMARY KEY
    (ProductCategoryID);
CLUSTER Production.ProductCategory USING "PK_ProductCategory_ProductCategoryID";

ALTER TABLE Production.ProductCostHistory ADD
    CONSTRAINT "PK_ProductCostHistory_ProductID_StartDate" PRIMARY KEY
    (ProductID, StartDate);
CLUSTER Production.ProductCostHistory USING "PK_ProductCostHistory_ProductID_StartDate";

ALTER TABLE Production.ProductDescription ADD
    CONSTRAINT "PK_ProductDescription_ProductDescriptionID" PRIMARY KEY
    (ProductDescriptionID);
CLUSTER Production.ProductDescription USING "PK_ProductDescription_ProductDescriptionID";

ALTER TABLE Production.ProductDocument ADD
    CONSTRAINT "PK_ProductDocument_ProductID_DocumentNode" PRIMARY KEY
    (ProductID, DocumentNode);
CLUSTER Production.ProductDocument USING "PK_ProductDocument_ProductID_DocumentNode";

ALTER TABLE Production.ProductInventory ADD
    CONSTRAINT "PK_ProductInventory_ProductID_LocationID" PRIMARY KEY
    (ProductID, LocationID);
CLUSTER Production.ProductInventory USING "PK_ProductInventory_ProductID_LocationID";

ALTER TABLE Production.ProductListPriceHistory ADD
    CONSTRAINT "PK_ProductListPriceHistory_ProductID_StartDate" PRIMARY KEY
    (ProductID, StartDate);
CLUSTER Production.ProductListPriceHistory USING "PK_ProductListPriceHistory_ProductID_StartDate";

ALTER TABLE Production.ProductModel ADD
    CONSTRAINT "PK_ProductModel_ProductModelID" PRIMARY KEY
    (ProductModelID);
CLUSTER Production.ProductModel USING "PK_ProductModel_ProductModelID";

ALTER TABLE Production.ProductModelIllustration ADD
    CONSTRAINT "PK_ProductModelIllustration_ProductModelID_IllustrationID" PRIMARY KEY
    (ProductModelID, IllustrationID);
CLUSTER Production.ProductModelIllustration USING "PK_ProductModelIllustration_ProductModelID_IllustrationID";

ALTER TABLE Production.ProductModelProductDescriptionCulture ADD
    CONSTRAINT "PK_ProductModelProductDescriptionCulture_ProductModelID_Product" PRIMARY KEY
    (ProductModelID, ProductDescriptionID, CultureID);
CLUSTER Production.ProductModelProductDescriptionCulture USING "PK_ProductModelProductDescriptionCulture_ProductModelID_Product";

ALTER TABLE Production.ProductPhoto ADD
    CONSTRAINT "PK_ProductPhoto_ProductPhotoID" PRIMARY KEY
    (ProductPhotoID);
CLUSTER Production.ProductPhoto USING "PK_ProductPhoto_ProductPhotoID";

ALTER TABLE Production.ProductProductPhoto ADD
    CONSTRAINT "PK_ProductProductPhoto_ProductID_ProductPhotoID" PRIMARY KEY
    (ProductID, ProductPhotoID);

ALTER TABLE Production.ProductReview ADD
    CONSTRAINT "PK_ProductReview_ProductReviewID" PRIMARY KEY
    (ProductReviewID);
CLUSTER Production.ProductReview USING "PK_ProductReview_ProductReviewID";

ALTER TABLE Production.ProductSubcategory ADD
    CONSTRAINT "PK_ProductSubcategory_ProductSubcategoryID" PRIMARY KEY
    (ProductSubcategoryID);
CLUSTER Production.ProductSubcategory USING "PK_ProductSubcategory_ProductSubcategoryID";

ALTER TABLE Purchasing.ProductVendor ADD
    CONSTRAINT "PK_ProductVendor_ProductID_BusinessEntityID" PRIMARY KEY
    (ProductID, BusinessEntityID);
CLUSTER Purchasing.ProductVendor USING "PK_ProductVendor_ProductID_BusinessEntityID";

ALTER TABLE Purchasing.PurchaseOrderDetail ADD
    CONSTRAINT "PK_PurchaseOrderDetail_PurchaseOrderID_PurchaseOrderDetailID" PRIMARY KEY
    (PurchaseOrderID, PurchaseOrderDetailID);
CLUSTER Purchasing.PurchaseOrderDetail USING "PK_PurchaseOrderDetail_PurchaseOrderID_PurchaseOrderDetailID";

ALTER TABLE Purchasing.PurchaseOrderHeader ADD
    CONSTRAINT "PK_PurchaseOrderHeader_PurchaseOrderID" PRIMARY KEY
    (PurchaseOrderID);
CLUSTER Purchasing.PurchaseOrderHeader USING "PK_PurchaseOrderHeader_PurchaseOrderID";

ALTER TABLE Sales.PersonCreditCard ADD
    CONSTRAINT "PK_PersonCreditCard_BusinessEntityID_CreditCardID" PRIMARY KEY
    (BusinessEntityID, CreditCardID);
CLUSTER Sales.PersonCreditCard USING "PK_PersonCreditCard_BusinessEntityID_CreditCardID";

ALTER TABLE Sales.SalesOrderDetail ADD
    CONSTRAINT "PK_SalesOrderDetail_SalesOrderID_SalesOrderDetailID" PRIMARY KEY
    (SalesOrderID, SalesOrderDetailID);
CLUSTER Sales.SalesOrderDetail USING "PK_SalesOrderDetail_SalesOrderID_SalesOrderDetailID";

ALTER TABLE Sales.SalesOrderHeader ADD
    CONSTRAINT "PK_SalesOrderHeader_SalesOrderID" PRIMARY KEY
    (SalesOrderID);
CLUSTER Sales.SalesOrderHeader USING "PK_SalesOrderHeader_SalesOrderID";

ALTER TABLE Sales.SalesOrderHeaderSalesReason ADD
    CONSTRAINT "PK_SalesOrderHeaderSalesReason_SalesOrderID_SalesReasonID" PRIMARY KEY
    (SalesOrderID, SalesReasonID);
CLUSTER Sales.SalesOrderHeaderSalesReason USING "PK_SalesOrderHeaderSalesReason_SalesOrderID_SalesReasonID";

ALTER TABLE Sales.SalesPerson ADD
    CONSTRAINT "PK_SalesPerson_BusinessEntityID" PRIMARY KEY
    (BusinessEntityID);
CLUSTER Sales.SalesPerson USING "PK_SalesPerson_BusinessEntityID";

ALTER TABLE Sales.SalesPersonQuotaHistory ADD
    CONSTRAINT "PK_SalesPersonQuotaHistory_BusinessEntityID_QuotaDate" PRIMARY KEY
    (BusinessEntityID, QuotaDate); -- ProductCategoryID);
CLUSTER Sales.SalesPersonQuotaHistory USING "PK_SalesPersonQuotaHistory_BusinessEntityID_QuotaDate";

ALTER TABLE Sales.SalesReason ADD
    CONSTRAINT "PK_SalesReason_SalesReasonID" PRIMARY KEY
    (SalesReasonID);
CLUSTER Sales.SalesReason USING "PK_SalesReason_SalesReasonID";

ALTER TABLE Sales.SalesTaxRate ADD
    CONSTRAINT "PK_SalesTaxRate_SalesTaxRateID" PRIMARY KEY
    (SalesTaxRateID);
CLUSTER Sales.SalesTaxRate USING "PK_SalesTaxRate_SalesTaxRateID";

ALTER TABLE Sales.SalesTerritory ADD
    CONSTRAINT "PK_SalesTerritory_TerritoryID" PRIMARY KEY
    (TerritoryID);
CLUSTER Sales.SalesTerritory USING "PK_SalesTerritory_TerritoryID";

ALTER TABLE Sales.SalesTerritoryHistory ADD
    CONSTRAINT "PK_SalesTerritoryHistory_BusinessEntityID_StartDate_TerritoryID" PRIMARY KEY
    (BusinessEntityID,  --Sales person,
     StartDate, TerritoryID);
CLUSTER Sales.SalesTerritoryHistory USING "PK_SalesTerritoryHistory_BusinessEntityID_StartDate_TerritoryID";

ALTER TABLE Production.ScrapReason ADD
    CONSTRAINT "PK_ScrapReason_ScrapReasonID" PRIMARY KEY
    (ScrapReasonID);
CLUSTER Production.ScrapReason USING "PK_ScrapReason_ScrapReasonID";

ALTER TABLE HumanResources.Shift ADD
    CONSTRAINT "PK_Shift_ShiftID" PRIMARY KEY
    (ShiftID);
CLUSTER HumanResources.Shift USING "PK_Shift_ShiftID";

ALTER TABLE Purchasing.ShipMethod ADD
    CONSTRAINT "PK_ShipMethod_ShipMethodID" PRIMARY KEY
    (ShipMethodID);
CLUSTER Purchasing.ShipMethod USING "PK_ShipMethod_ShipMethodID";

ALTER TABLE Sales.ShoppingCartItem ADD
    CONSTRAINT "PK_ShoppingCartItem_ShoppingCartItemID" PRIMARY KEY
    (ShoppingCartItemID);
CLUSTER Sales.ShoppingCartItem USING "PK_ShoppingCartItem_ShoppingCartItemID";

ALTER TABLE Sales.SpecialOffer ADD
    CONSTRAINT "PK_SpecialOffer_SpecialOfferID" PRIMARY KEY
    (SpecialOfferID);
CLUSTER Sales.SpecialOffer USING "PK_SpecialOffer_SpecialOfferID";

ALTER TABLE Sales.SpecialOfferProduct ADD
    CONSTRAINT "PK_SpecialOfferProduct_SpecialOfferID_ProductID" PRIMARY KEY
    (SpecialOfferID, ProductID);
CLUSTER Sales.SpecialOfferProduct USING "PK_SpecialOfferProduct_SpecialOfferID_ProductID";

ALTER TABLE Person.StateProvince ADD
    CONSTRAINT "PK_StateProvince_StateProvinceID" PRIMARY KEY
    (StateProvinceID);
CLUSTER Person.StateProvince USING "PK_StateProvince_StateProvinceID";

ALTER TABLE Sales.Store ADD
    CONSTRAINT "PK_Store_BusinessEntityID" PRIMARY KEY
    (BusinessEntityID);
CLUSTER Sales.Store USING "PK_Store_BusinessEntityID";

ALTER TABLE Production.TransactionHistory ADD
    CONSTRAINT "PK_TransactionHistory_TransactionID" PRIMARY KEY
    (TransactionID);
CLUSTER Production.TransactionHistory USING "PK_TransactionHistory_TransactionID";

ALTER TABLE Production.TransactionHistoryArchive ADD
    CONSTRAINT "PK_TransactionHistoryArchive_TransactionID" PRIMARY KEY
    (TransactionID);
CLUSTER Production.TransactionHistoryArchive USING "PK_TransactionHistoryArchive_TransactionID";

ALTER TABLE Production.UnitMeasure ADD
    CONSTRAINT "PK_UnitMeasure_UnitMeasureCode" PRIMARY KEY
    (UnitMeasureCode);
CLUSTER Production.UnitMeasure USING "PK_UnitMeasure_UnitMeasureCode";

ALTER TABLE Purchasing.Vendor ADD
    CONSTRAINT "PK_Vendor_BusinessEntityID" PRIMARY KEY
    (BusinessEntityID);
CLUSTER Purchasing.Vendor USING "PK_Vendor_BusinessEntityID";

ALTER TABLE Production.WorkOrder ADD
    CONSTRAINT "PK_WorkOrder_WorkOrderID" PRIMARY KEY
    (WorkOrderID);
CLUSTER Production.WorkOrder USING "PK_WorkOrder_WorkOrderID";

ALTER TABLE Production.WorkOrderRouting ADD
    CONSTRAINT "PK_WorkOrderRouting_WorkOrderID_ProductID_OperationSequence" PRIMARY KEY
    (WorkOrderID, ProductID, OperationSequence);
CLUSTER Production.WorkOrderRouting USING "PK_WorkOrderRouting_WorkOrderID_ProductID_OperationSequence";



-------------------------------------
-- FOREIGN KEYS
-------------------------------------

ALTER TABLE Person.Address ADD
    CONSTRAINT "FK_Address_StateProvince_StateProvinceID" FOREIGN KEY
    (StateProvinceID) REFERENCES Person.StateProvince(StateProvinceID);

ALTER TABLE Production.BillOfMaterials ADD
    CONSTRAINT "FK_BillOfMaterials_Product_ProductAssemblyID" FOREIGN KEY
    (ProductAssemblyID) REFERENCES Production.Product(ProductID);
ALTER TABLE Production.BillOfMaterials ADD
    CONSTRAINT "FK_BillOfMaterials_Product_ComponentID" FOREIGN KEY
    (ComponentID) REFERENCES Production.Product(ProductID);
ALTER TABLE Production.BillOfMaterials ADD
    CONSTRAINT "FK_BillOfMaterials_UnitMeasure_UnitMeasureCode" FOREIGN KEY
    (UnitMeasureCode) REFERENCES Production.UnitMeasure(UnitMeasureCode);

ALTER TABLE Person.BusinessEntityAddress ADD
    CONSTRAINT "FK_BusinessEntityAddress_Address_AddressID" FOREIGN KEY
    (AddressID) REFERENCES Person.Address(AddressID);
ALTER TABLE Person.BusinessEntityAddress ADD
    CONSTRAINT "FK_BusinessEntityAddress_AddressType_AddressTypeID" FOREIGN KEY
    (AddressTypeID) REFERENCES Person.AddressType(AddressTypeID);
ALTER TABLE Person.BusinessEntityAddress ADD
    CONSTRAINT "FK_BusinessEntityAddress_BusinessEntity_BusinessEntityID" FOREIGN KEY
    (BusinessEntityID) REFERENCES Person.BusinessEntity(BusinessEntityID);

ALTER TABLE Person.BusinessEntityContact ADD
    CONSTRAINT "FK_BusinessEntityContact_Person_PersonID" FOREIGN KEY
    (PersonID) REFERENCES Person.Person(BusinessEntityID);
ALTER TABLE Person.BusinessEntityContact ADD
    CONSTRAINT "FK_BusinessEntityContact_ContactType_ContactTypeID" FOREIGN KEY
    (ContactTypeID) REFERENCES Person.ContactType(ContactTypeID);
ALTER TABLE Person.BusinessEntityContact ADD
    CONSTRAINT "FK_BusinessEntityContact_BusinessEntity_BusinessEntityID" FOREIGN KEY
    (BusinessEntityID) REFERENCES Person.BusinessEntity(BusinessEntityID);

ALTER TABLE Sales.CountryRegionCurrency ADD
    CONSTRAINT "FK_CountryRegionCurrency_CountryRegion_CountryRegionCode" FOREIGN KEY
    (CountryRegionCode) REFERENCES Person.CountryRegion(CountryRegionCode);
ALTER TABLE Sales.CountryRegionCurrency ADD
    CONSTRAINT "FK_CountryRegionCurrency_Currency_CurrencyCode" FOREIGN KEY
    (CurrencyCode) REFERENCES Sales.Currency(CurrencyCode);

ALTER TABLE Sales.CurrencyRate ADD
    CONSTRAINT "FK_CurrencyRate_Currency_FromCurrencyCode" FOREIGN KEY
    (FromCurrencyCode) REFERENCES Sales.Currency(CurrencyCode);
ALTER TABLE Sales.CurrencyRate ADD
    CONSTRAINT "FK_CurrencyRate_Currency_ToCurrencyCode" FOREIGN KEY
    (ToCurrencyCode) REFERENCES Sales.Currency(CurrencyCode);

ALTER TABLE Sales.Customer ADD
    CONSTRAINT "FK_Customer_Person_PersonID" FOREIGN KEY
    (PersonID) REFERENCES Person.Person(BusinessEntityID);
ALTER TABLE Sales.Customer ADD
    CONSTRAINT "FK_Customer_Store_StoreID" FOREIGN KEY
    (StoreID) REFERENCES Sales.Store(BusinessEntityID);
ALTER TABLE Sales.Customer ADD
    CONSTRAINT "FK_Customer_SalesTerritory_TerritoryID" FOREIGN KEY
    (TerritoryID) REFERENCES Sales.SalesTerritory(TerritoryID);

ALTER TABLE Production.Document ADD
    CONSTRAINT "FK_Document_Employee_Owner" FOREIGN KEY
    (Owner) REFERENCES HumanResources.Employee(BusinessEntityID);

ALTER TABLE Person.EmailAddress ADD
    CONSTRAINT "FK_EmailAddress_Person_BusinessEntityID" FOREIGN KEY
    (BusinessEntityID) REFERENCES Person.Person(BusinessEntityID);

ALTER TABLE HumanResources.Employee ADD
    CONSTRAINT "FK_Employee_Person_BusinessEntityID" FOREIGN KEY
    (BusinessEntityID) REFERENCES Person.Person(BusinessEntityID);

ALTER TABLE HumanResources.EmployeeDepartmentHistory ADD
    CONSTRAINT "FK_EmployeeDepartmentHistory_Department_DepartmentID" FOREIGN KEY
    (DepartmentID) REFERENCES HumanResources.Department(DepartmentID);
ALTER TABLE HumanResources.EmployeeDepartmentHistory ADD
    CONSTRAINT "FK_EmployeeDepartmentHistory_Employee_BusinessEntityID" FOREIGN KEY
    (BusinessEntityID) REFERENCES HumanResources.Employee(BusinessEntityID);
ALTER TABLE HumanResources.EmployeeDepartmentHistory ADD
    CONSTRAINT "FK_EmployeeDepartmentHistory_Shift_ShiftID" FOREIGN KEY
    (ShiftID) REFERENCES HumanResources.Shift(ShiftID);

ALTER TABLE HumanResources.EmployeePayHistory ADD
    CONSTRAINT "FK_EmployeePayHistory_Employee_BusinessEntityID" FOREIGN KEY
    (BusinessEntityID) REFERENCES HumanResources.Employee(BusinessEntityID);

ALTER TABLE HumanResources.JobCandidate ADD
    CONSTRAINT "FK_JobCandidate_Employee_BusinessEntityID" FOREIGN KEY
    (BusinessEntityID) REFERENCES HumanResources.Employee(BusinessEntityID);

ALTER TABLE Person.Password ADD
    CONSTRAINT "FK_Password_Person_BusinessEntityID" FOREIGN KEY
    (BusinessEntityID) REFERENCES Person.Person(BusinessEntityID);

ALTER TABLE Person.Person ADD
    CONSTRAINT "FK_Person_BusinessEntity_BusinessEntityID" FOREIGN KEY
    (BusinessEntityID) REFERENCES Person.BusinessEntity(BusinessEntityID);

ALTER TABLE Sales.PersonCreditCard ADD
    CONSTRAINT "FK_PersonCreditCard_Person_BusinessEntityID" FOREIGN KEY
    (BusinessEntityID) REFERENCES Person.Person(BusinessEntityID);
ALTER TABLE Sales.PersonCreditCard ADD
    CONSTRAINT "FK_PersonCreditCard_CreditCard_CreditCardID" FOREIGN KEY
    (CreditCardID) REFERENCES Sales.CreditCard(CreditCardID);

ALTER TABLE Person.PersonPhone ADD
    CONSTRAINT "FK_PersonPhone_Person_BusinessEntityID" FOREIGN KEY
    (BusinessEntityID) REFERENCES Person.Person(BusinessEntityID);
ALTER TABLE Person.PersonPhone ADD
    CONSTRAINT "FK_PersonPhone_PhoneNumberType_PhoneNumberTypeID" FOREIGN KEY
    (PhoneNumberTypeID) REFERENCES Person.PhoneNumberType(PhoneNumberTypeID);

ALTER TABLE Production.Product ADD
    CONSTRAINT "FK_Product_UnitMeasure_SizeUnitMeasureCode" FOREIGN KEY
    (SizeUnitMeasureCode) REFERENCES Production.UnitMeasure(UnitMeasureCode);
ALTER TABLE Production.Product ADD
    CONSTRAINT "FK_Product_UnitMeasure_WeightUnitMeasureCode" FOREIGN KEY
    (WeightUnitMeasureCode) REFERENCES Production.UnitMeasure(UnitMeasureCode);
ALTER TABLE Production.Product ADD
    CONSTRAINT "FK_Product_ProductModel_ProductModelID" FOREIGN KEY
    (ProductModelID) REFERENCES Production.ProductModel(ProductModelID);
ALTER TABLE Production.Product ADD
    CONSTRAINT "FK_Product_ProductSubcategory_ProductSubcategoryID" FOREIGN KEY
    (ProductSubcategoryID) REFERENCES Production.ProductSubcategory(ProductSubcategoryID);

ALTER TABLE Production.ProductCostHistory ADD
    CONSTRAINT "FK_ProductCostHistory_Product_ProductID" FOREIGN KEY
    (ProductID) REFERENCES Production.Product(ProductID);

ALTER TABLE Production.ProductDocument ADD
    CONSTRAINT "FK_ProductDocument_Product_ProductID" FOREIGN KEY
    (ProductID) REFERENCES Production.Product(ProductID);
ALTER TABLE Production.ProductDocument ADD
    CONSTRAINT "FK_ProductDocument_Document_DocumentNode" FOREIGN KEY
    (DocumentNode) REFERENCES Production.Document(DocumentNode);

ALTER TABLE Production.ProductInventory ADD
    CONSTRAINT "FK_ProductInventory_Location_LocationID" FOREIGN KEY
    (LocationID) REFERENCES Production.Location(LocationID);
ALTER TABLE Production.ProductInventory ADD
    CONSTRAINT "FK_ProductInventory_Product_ProductID" FOREIGN KEY
    (ProductID) REFERENCES Production.Product(ProductID);

ALTER TABLE Production.ProductListPriceHistory ADD
    CONSTRAINT "FK_ProductListPriceHistory_Product_ProductID" FOREIGN KEY
    (ProductID) REFERENCES Production.Product(ProductID);

ALTER TABLE Production.ProductModelIllustration ADD
    CONSTRAINT "FK_ProductModelIllustration_ProductModel_ProductModelID" FOREIGN KEY
    (ProductModelID) REFERENCES Production.ProductModel(ProductModelID);
ALTER TABLE Production.ProductModelIllustration ADD
    CONSTRAINT "FK_ProductModelIllustration_Illustration_IllustrationID" FOREIGN KEY
    (IllustrationID) REFERENCES Production.Illustration(IllustrationID);

ALTER TABLE Production.ProductModelProductDescriptionCulture ADD
    CONSTRAINT "FK_ProductModelProductDescriptionCulture_ProductDescription_Pro" FOREIGN KEY
    (ProductDescriptionID) REFERENCES Production.ProductDescription(ProductDescriptionID);
ALTER TABLE Production.ProductModelProductDescriptionCulture ADD
    CONSTRAINT "FK_ProductModelProductDescriptionCulture_Culture_CultureID" FOREIGN KEY
    (CultureID) REFERENCES Production.Culture(CultureID);
ALTER TABLE Production.ProductModelProductDescriptionCulture ADD
    CONSTRAINT "FK_ProductModelProductDescriptionCulture_ProductModel_ProductMo" FOREIGN KEY
    (ProductModelID) REFERENCES Production.ProductModel(ProductModelID);

ALTER TABLE Production.ProductProductPhoto ADD
    CONSTRAINT "FK_ProductProductPhoto_Product_ProductID" FOREIGN KEY
    (ProductID) REFERENCES Production.Product(ProductID);
ALTER TABLE Production.ProductProductPhoto ADD
    CONSTRAINT "FK_ProductProductPhoto_ProductPhoto_ProductPhotoID" FOREIGN KEY
    (ProductPhotoID) REFERENCES Production.ProductPhoto(ProductPhotoID);

ALTER TABLE Production.ProductReview ADD
    CONSTRAINT "FK_ProductReview_Product_ProductID" FOREIGN KEY
    (ProductID) REFERENCES Production.Product(ProductID);

ALTER TABLE Production.ProductSubcategory ADD
    CONSTRAINT "FK_ProductSubcategory_ProductCategory_ProductCategoryID" FOREIGN KEY
    (ProductCategoryID) REFERENCES Production.ProductCategory(ProductCategoryID);

ALTER TABLE Purchasing.ProductVendor ADD
    CONSTRAINT "FK_ProductVendor_Product_ProductID" FOREIGN KEY
    (ProductID) REFERENCES Production.Product(ProductID);
ALTER TABLE Purchasing.ProductVendor ADD
    CONSTRAINT "FK_ProductVendor_UnitMeasure_UnitMeasureCode" FOREIGN KEY
    (UnitMeasureCode) REFERENCES Production.UnitMeasure(UnitMeasureCode);
ALTER TABLE Purchasing.ProductVendor ADD
    CONSTRAINT "FK_ProductVendor_Vendor_BusinessEntityID" FOREIGN KEY
    (BusinessEntityID) REFERENCES Purchasing.Vendor(BusinessEntityID);

ALTER TABLE Purchasing.PurchaseOrderDetail ADD
    CONSTRAINT "FK_PurchaseOrderDetail_Product_ProductID" FOREIGN KEY
    (ProductID) REFERENCES Production.Product(ProductID);
ALTER TABLE Purchasing.PurchaseOrderDetail ADD
    CONSTRAINT "FK_PurchaseOrderDetail_PurchaseOrderHeader_PurchaseOrderID" FOREIGN KEY
    (PurchaseOrderID) REFERENCES Purchasing.PurchaseOrderHeader(PurchaseOrderID);

ALTER TABLE Purchasing.PurchaseOrderHeader ADD
    CONSTRAINT "FK_PurchaseOrderHeader_Employee_EmployeeID" FOREIGN KEY
    (EmployeeID) REFERENCES HumanResources.Employee(BusinessEntityID);
ALTER TABLE Purchasing.PurchaseOrderHeader ADD
    CONSTRAINT "FK_PurchaseOrderHeader_Vendor_VendorID" FOREIGN KEY
    (VendorID) REFERENCES Purchasing.Vendor(BusinessEntityID);
ALTER TABLE Purchasing.PurchaseOrderHeader ADD
    CONSTRAINT "FK_PurchaseOrderHeader_ShipMethod_ShipMethodID" FOREIGN KEY
    (ShipMethodID) REFERENCES Purchasing.ShipMethod(ShipMethodID);

ALTER TABLE Sales.SalesOrderDetail ADD
    CONSTRAINT "FK_SalesOrderDetail_SalesOrderHeader_SalesOrderID" FOREIGN KEY
    (SalesOrderID) REFERENCES Sales.SalesOrderHeader(SalesOrderID) ON DELETE CASCADE;
ALTER TABLE Sales.SalesOrderDetail ADD
    CONSTRAINT "FK_SalesOrderDetail_SpecialOfferProduct_SpecialOfferIDProductID" FOREIGN KEY
    (SpecialOfferID, ProductID) REFERENCES Sales.SpecialOfferProduct(SpecialOfferID, ProductID);

ALTER TABLE Sales.SalesOrderHeader ADD
    CONSTRAINT "FK_SalesOrderHeader_Address_BillToAddressID" FOREIGN KEY
    (BillToAddressID) REFERENCES Person.Address(AddressID);
ALTER TABLE Sales.SalesOrderHeader ADD
    CONSTRAINT "FK_SalesOrderHeader_Address_ShipToAddressID" FOREIGN KEY
    (ShipToAddressID) REFERENCES Person.Address(AddressID);
ALTER TABLE Sales.SalesOrderHeader ADD
    CONSTRAINT "FK_SalesOrderHeader_CreditCard_CreditCardID" FOREIGN KEY
    (CreditCardID) REFERENCES Sales.CreditCard(CreditCardID);
ALTER TABLE Sales.SalesOrderHeader ADD
    CONSTRAINT "FK_SalesOrderHeader_CurrencyRate_CurrencyRateID" FOREIGN KEY
    (CurrencyRateID) REFERENCES Sales.CurrencyRate(CurrencyRateID);
ALTER TABLE Sales.SalesOrderHeader ADD
    CONSTRAINT "FK_SalesOrderHeader_Customer_CustomerID" FOREIGN KEY
    (CustomerID) REFERENCES Sales.Customer(CustomerID);
ALTER TABLE Sales.SalesOrderHeader ADD
    CONSTRAINT "FK_SalesOrderHeader_SalesPerson_SalesPersonID" FOREIGN KEY
    (SalesPersonID) REFERENCES Sales.SalesPerson(BusinessEntityID);
ALTER TABLE Sales.SalesOrderHeader ADD
    CONSTRAINT "FK_SalesOrderHeader_ShipMethod_ShipMethodID" FOREIGN KEY
    (ShipMethodID) REFERENCES Purchasing.ShipMethod(ShipMethodID);
ALTER TABLE Sales.SalesOrderHeader ADD
    CONSTRAINT "FK_SalesOrderHeader_SalesTerritory_TerritoryID" FOREIGN KEY
    (TerritoryID) REFERENCES Sales.SalesTerritory(TerritoryID);

ALTER TABLE Sales.SalesOrderHeaderSalesReason ADD
    CONSTRAINT "FK_SalesOrderHeaderSalesReason_SalesReason_SalesReasonID" FOREIGN KEY
    (SalesReasonID) REFERENCES Sales.SalesReason(SalesReasonID);
ALTER TABLE Sales.SalesOrderHeaderSalesReason ADD
    CONSTRAINT "FK_SalesOrderHeaderSalesReason_SalesOrderHeader_SalesOrderID" FOREIGN KEY
    (SalesOrderID) REFERENCES Sales.SalesOrderHeader(SalesOrderID) ON DELETE CASCADE;

ALTER TABLE Sales.SalesPerson ADD
    CONSTRAINT "FK_SalesPerson_Employee_BusinessEntityID" FOREIGN KEY
    (BusinessEntityID) REFERENCES HumanResources.Employee(BusinessEntityID);
ALTER TABLE Sales.SalesPerson ADD
    CONSTRAINT "FK_SalesPerson_SalesTerritory_TerritoryID" FOREIGN KEY
    (TerritoryID) REFERENCES Sales.SalesTerritory(TerritoryID);

ALTER TABLE Sales.SalesPersonQuotaHistory ADD
    CONSTRAINT "FK_SalesPersonQuotaHistory_SalesPerson_BusinessEntityID" FOREIGN KEY
    (BusinessEntityID) REFERENCES Sales.SalesPerson(BusinessEntityID);

ALTER TABLE Sales.SalesTaxRate ADD
    CONSTRAINT "FK_SalesTaxRate_StateProvince_StateProvinceID" FOREIGN KEY
    (StateProvinceID) REFERENCES Person.StateProvince(StateProvinceID);

ALTER TABLE Sales.SalesTerritory ADD
    CONSTRAINT "FK_SalesTerritory_CountryRegion_CountryRegionCode" FOREIGN KEY
    (CountryRegionCode) REFERENCES Person.CountryRegion(CountryRegionCode);

ALTER TABLE Sales.SalesTerritoryHistory ADD
    CONSTRAINT "FK_SalesTerritoryHistory_SalesPerson_BusinessEntityID" FOREIGN KEY
    (BusinessEntityID) REFERENCES Sales.SalesPerson(BusinessEntityID);
ALTER TABLE Sales.SalesTerritoryHistory ADD
    CONSTRAINT "FK_SalesTerritoryHistory_SalesTerritory_TerritoryID" FOREIGN KEY
    (TerritoryID) REFERENCES Sales.SalesTerritory(TerritoryID);

ALTER TABLE Sales.ShoppingCartItem ADD
    CONSTRAINT "FK_ShoppingCartItem_Product_ProductID" FOREIGN KEY
    (ProductID) REFERENCES Production.Product(ProductID);

ALTER TABLE Sales.SpecialOfferProduct ADD
    CONSTRAINT "FK_SpecialOfferProduct_Product_ProductID" FOREIGN KEY
    (ProductID) REFERENCES Production.Product(ProductID);
ALTER TABLE Sales.SpecialOfferProduct ADD
    CONSTRAINT "FK_SpecialOfferProduct_SpecialOffer_SpecialOfferID" FOREIGN KEY
    (SpecialOfferID) REFERENCES Sales.SpecialOffer(SpecialOfferID);

ALTER TABLE Person.StateProvince ADD
    CONSTRAINT "FK_StateProvince_CountryRegion_CountryRegionCode" FOREIGN KEY
    (CountryRegionCode) REFERENCES Person.CountryRegion(CountryRegionCode);
ALTER TABLE Person.StateProvince ADD
    CONSTRAINT "FK_StateProvince_SalesTerritory_TerritoryID" FOREIGN KEY
    (TerritoryID) REFERENCES Sales.SalesTerritory(TerritoryID);

ALTER TABLE Sales.Store ADD
    CONSTRAINT "FK_Store_BusinessEntity_BusinessEntityID" FOREIGN KEY
    (BusinessEntityID) REFERENCES Person.BusinessEntity(BusinessEntityID);
ALTER TABLE Sales.Store ADD
    CONSTRAINT "FK_Store_SalesPerson_SalesPersonID" FOREIGN KEY
    (SalesPersonID) REFERENCES Sales.SalesPerson(BusinessEntityID);

ALTER TABLE Production.TransactionHistory ADD
    CONSTRAINT "FK_TransactionHistory_Product_ProductID" FOREIGN KEY
    (ProductID) REFERENCES Production.Product(ProductID);

ALTER TABLE Purchasing.Vendor ADD
    CONSTRAINT "FK_Vendor_BusinessEntity_BusinessEntityID" FOREIGN KEY
    (BusinessEntityID) REFERENCES Person.BusinessEntity(BusinessEntityID);

ALTER TABLE Production.WorkOrder ADD
    CONSTRAINT "FK_WorkOrder_Product_ProductID" FOREIGN KEY
    (ProductID) REFERENCES Production.Product(ProductID);
ALTER TABLE Production.WorkOrder ADD
    CONSTRAINT "FK_WorkOrder_ScrapReason_ScrapReasonID" FOREIGN KEY
    (ScrapReasonID) REFERENCES Production.ScrapReason(ScrapReasonID);

ALTER TABLE Production.WorkOrderRouting ADD
    CONSTRAINT "FK_WorkOrderRouting_Location_LocationID" FOREIGN KEY
    (LocationID) REFERENCES Production.Location(LocationID);
ALTER TABLE Production.WorkOrderRouting ADD
    CONSTRAINT "FK_WorkOrderRouting_WorkOrder_WorkOrderID" FOREIGN KEY
    (WorkOrderID) REFERENCES Production.WorkOrder(WorkOrderID);



-------------------------------------
-- VIEWS
-------------------------------------

-- Fun to see the difference in XML-oriented queries between MSSQLServer and Postgres.
-- First here's an original MSSQL query:

-- CREATE VIEW [Person].[vAdditionalContactInfo]
-- AS
-- SELECT
--     [BusinessEntityID]
--     ,[FirstName]
--     ,[MiddleName]
--     ,[LastName]
--     ,[ContactInfo].ref.value(N'declare namespace ci="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactInfo";
--         declare namespace act="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes";
--         (act:telephoneNumber)[1]/act:number', 'nvarchar(50)') AS [TelephoneNumber]
--     ,LTRIM(RTRIM([ContactInfo].ref.value(N'declare namespace ci="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactInfo";
--         declare namespace act="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes";
--         (act:telephoneNumber/act:SpecialInstructions/text())[1]', 'nvarchar(max)'))) AS [TelephoneSpecialInstructions]
--     ,[ContactInfo].ref.value(N'declare namespace ci="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactInfo";
--         declare namespace act="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes";
--         (act:homePostalAddress/act:Street)[1]', 'nvarchar(50)') AS [Street]
--     ,[ContactInfo].ref.value(N'declare namespace ci="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactInfo";
--         declare namespace act="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes";
--         (act:homePostalAddress/act:City)[1]', 'nvarchar(50)') AS [City]
--     ,[ContactInfo].ref.value(N'declare namespace ci="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactInfo";
--         declare namespace act="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes";
--         (act:homePostalAddress/act:StateProvince)[1]', 'nvarchar(50)') AS [StateProvince]
--     ,[ContactInfo].ref.value(N'declare namespace ci="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactInfo";
--         declare namespace act="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes";
--         (act:homePostalAddress/act:PostalCode)[1]', 'nvarchar(50)') AS [PostalCode]
--     ,[ContactInfo].ref.value(N'declare namespace ci="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactInfo";
--         declare namespace act="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes";
--         (act:homePostalAddress/act:CountryRegion)[1]', 'nvarchar(50)') AS [CountryRegion]
--     ,[ContactInfo].ref.value(N'declare namespace ci="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactInfo";
--         declare namespace act="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes";
--         (act:homePostalAddress/act:SpecialInstructions/text())[1]', 'nvarchar(max)') AS [HomeAddressSpecialInstructions]
--     ,[ContactInfo].ref.value(N'declare namespace ci="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactInfo";
--         declare namespace act="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes";
--         (act:eMail/act:eMailAddress)[1]', 'nvarchar(128)') AS [EMailAddress]
--     ,LTRIM(RTRIM([ContactInfo].ref.value(N'declare namespace ci="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactInfo";
--         declare namespace act="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes";
--         (act:eMail/act:SpecialInstructions/text())[1]', 'nvarchar(max)'))) AS [EMailSpecialInstructions]
--     ,[ContactInfo].ref.value(N'declare namespace ci="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactInfo";
--         declare namespace act="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes";
--         (act:eMail/act:SpecialInstructions/act:telephoneNumber/act:number)[1]', 'nvarchar(50)') AS [EMailTelephoneNumber]
--     ,[rowguid]
--     ,[ModifiedDate]
-- FROM [Person].[Person]
-- OUTER APPLY [AdditionalContactInfo].nodes(
--     'declare namespace ci="http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactInfo";
--     /ci:AdditionalContactInfo') AS ContactInfo(ref)
-- WHERE [AdditionalContactInfo] IS NOT NULL;


-- And now the Postgres version, which is a little more trim:

CREATE VIEW Person.vAdditionalContactInfo
AS
SELECT
    p.BusinessEntityID
    ,p.FirstName
    ,p.MiddleName
    ,p.LastName
    ,(xpath('(act:telephoneNumber)[1]/act:number/text()',                node, '{{act,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes}}'))[1]
               AS TelephoneNumber
    ,BTRIM(
     (xpath('(act:telephoneNumber)[1]/act:SpecialInstructions/text()',   node, '{{act,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes}}'))[1]::VARCHAR)
               AS TelephoneSpecialInstructions
    ,(xpath('(act:homePostalAddress)[1]/act:Street/text()',              node, '{{act,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes}}'))[1]
               AS Street
    ,(xpath('(act:homePostalAddress)[1]/act:City/text()',                node, '{{act,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes}}'))[1]
               AS City
    ,(xpath('(act:homePostalAddress)[1]/act:StateProvince/text()',       node, '{{act,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes}}'))[1]
               AS StateProvince
    ,(xpath('(act:homePostalAddress)[1]/act:PostalCode/text()',          node, '{{act,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes}}'))[1]
               AS PostalCode
    ,(xpath('(act:homePostalAddress)[1]/act:CountryRegion/text()',       node, '{{act,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes}}'))[1]
               AS CountryRegion
    ,(xpath('(act:homePostalAddress)[1]/act:SpecialInstructions/text()', node, '{{act,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes}}'))[1]
               AS HomeAddressSpecialInstructions
    ,(xpath('(act:eMail)[1]/act:eMailAddress/text()',                    node, '{{act,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes}}'))[1]
               AS EMailAddress
    ,BTRIM(
     (xpath('(act:eMail)[1]/act:SpecialInstructions/text()',             node, '{{act,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes}}'))[1]::VARCHAR)
               AS EMailSpecialInstructions
    ,(xpath('((act:eMail)[1]/act:SpecialInstructions/act:telephoneNumber)[1]/act:number/text()', node, '{{act,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactTypes}}'))[1]
               AS EMailTelephoneNumber
    ,p.rowguid
    ,p.ModifiedDate
FROM Person.Person AS p
  LEFT OUTER JOIN
    (SELECT
      BusinessEntityID
      ,UNNEST(xpath('/ci:AdditionalContactInfo',
        additionalcontactinfo,
        '{{ci,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ContactInfo}}')) AS node
    FROM Person.Person
    WHERE AdditionalContactInfo IS NOT NULL) AS additional
  ON p.BusinessEntityID = additional.BusinessEntityID;


CREATE VIEW HumanResources.vEmployee
AS
SELECT
    e.BusinessEntityID
    ,p.Title
    ,p.FirstName
    ,p.MiddleName
    ,p.LastName
    ,p.Suffix
    ,e.JobTitle 
    ,pp.PhoneNumber
    ,pnt.Name AS PhoneNumberType
    ,ea.EmailAddress
    ,p.EmailPromotion
    ,a.AddressLine1
    ,a.AddressLine2
    ,a.City
    ,sp.Name AS StateProvinceName
    ,a.PostalCode
    ,cr.Name AS CountryRegionName
    ,p.AdditionalContactInfo
FROM HumanResources.Employee e
  INNER JOIN Person.Person p
    ON p.BusinessEntityID = e.BusinessEntityID
  INNER JOIN Person.BusinessEntityAddress bea
    ON bea.BusinessEntityID = e.BusinessEntityID
  INNER JOIN Person.Address a
    ON a.AddressID = bea.AddressID
  INNER JOIN Person.StateProvince sp
    ON sp.StateProvinceID = a.StateProvinceID
  INNER JOIN Person.CountryRegion cr
    ON cr.CountryRegionCode = sp.CountryRegionCode
  LEFT OUTER JOIN Person.PersonPhone pp
    ON pp.BusinessEntityID = p.BusinessEntityID
  LEFT OUTER JOIN Person.PhoneNumberType pnt
    ON pp.PhoneNumberTypeID = pnt.PhoneNumberTypeID
  LEFT OUTER JOIN Person.EmailAddress ea
    ON p.BusinessEntityID = ea.BusinessEntityID;


CREATE VIEW HumanResources.vEmployeeDepartment
AS
SELECT
    e.BusinessEntityID
    ,p.Title
    ,p.FirstName
    ,p.MiddleName
    ,p.LastName
    ,p.Suffix
    ,e.JobTitle
    ,d.Name AS Department
    ,d.GroupName
    ,edh.StartDate
FROM HumanResources.Employee e
  INNER JOIN Person.Person p
    ON p.BusinessEntityID = e.BusinessEntityID
  INNER JOIN HumanResources.EmployeeDepartmentHistory edh
    ON e.BusinessEntityID = edh.BusinessEntityID
  INNER JOIN HumanResources.Department d
    ON edh.DepartmentID = d.DepartmentID
WHERE edh.EndDate IS NULL;


CREATE VIEW HumanResources.vEmployeeDepartmentHistory
AS
SELECT
    e.BusinessEntityID
    ,p.Title
    ,p.FirstName
    ,p.MiddleName
    ,p.LastName
    ,p.Suffix
    ,s.Name AS Shift
    ,d.Name AS Department
    ,d.GroupName
    ,edh.StartDate
    ,edh.EndDate
FROM HumanResources.Employee e
  INNER JOIN Person.Person p
    ON p.BusinessEntityID = e.BusinessEntityID
  INNER JOIN HumanResources.EmployeeDepartmentHistory edh
    ON e.BusinessEntityID = edh.BusinessEntityID
  INNER JOIN HumanResources.Department d
    ON edh.DepartmentID = d.DepartmentID
  INNER JOIN HumanResources.Shift s
    ON s.ShiftID = edh.ShiftID;


CREATE VIEW Sales.vIndividualCustomer
AS
SELECT
    p.BusinessEntityID
    ,p.Title
    ,p.FirstName
    ,p.MiddleName
    ,p.LastName
    ,p.Suffix
    ,pp.PhoneNumber
    ,pnt.Name AS PhoneNumberType
    ,ea.EmailAddress
    ,p.EmailPromotion
    ,at.Name AS AddressType
    ,a.AddressLine1
    ,a.AddressLine2
    ,a.City
    ,sp.Name AS StateProvinceName
    ,a.PostalCode
    ,cr.Name AS CountryRegionName
    ,p.Demographics
FROM Person.Person p
  INNER JOIN Person.BusinessEntityAddress bea
    ON bea.BusinessEntityID = p.BusinessEntityID
  INNER JOIN Person.Address a
    ON a.AddressID = bea.AddressID
  INNER JOIN Person.StateProvince sp
    ON sp.StateProvinceID = a.StateProvinceID
  INNER JOIN Person.CountryRegion cr
    ON cr.CountryRegionCode = sp.CountryRegionCode
  INNER JOIN Person.AddressType at
    ON at.AddressTypeID = bea.AddressTypeID
  INNER JOIN Sales.Customer c
    ON c.PersonID = p.BusinessEntityID
  LEFT OUTER JOIN Person.EmailAddress ea
    ON ea.BusinessEntityID = p.BusinessEntityID
  LEFT OUTER JOIN Person.PersonPhone pp
    ON pp.BusinessEntityID = p.BusinessEntityID
  LEFT OUTER JOIN Person.PhoneNumberType pnt
    ON pnt.PhoneNumberTypeID = pp.PhoneNumberTypeID
WHERE c.StoreID IS NULL;


CREATE VIEW Sales.vPersonDemographics
AS
SELECT
    BusinessEntityID
    ,CAST((xpath('n:TotalPurchaseYTD/text()', Demographics, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/IndividualSurvey}}'))[1]::VARCHAR AS money)
            AS TotalPurchaseYTD
    ,CAST((xpath('n:DateFirstPurchase/text()', Demographics, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/IndividualSurvey}}'))[1]::VARCHAR AS DATE)
            AS DateFirstPurchase
    ,CAST((xpath('n:BirthDate/text()', Demographics, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/IndividualSurvey}}'))[1]::VARCHAR AS DATE)
            AS BirthDate
    ,(xpath('n:MaritalStatus/text()', Demographics, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/IndividualSurvey}}'))[1]::VARCHAR(1)
            AS MaritalStatus
    ,(xpath('n:YearlyIncome/text()', Demographics, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/IndividualSurvey}}'))[1]::VARCHAR(30)
            AS YearlyIncome
    ,(xpath('n:Gender/text()', Demographics, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/IndividualSurvey}}'))[1]::VARCHAR(1)
            AS Gender
    ,CAST((xpath('n:TotalChildren/text()', Demographics, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/IndividualSurvey}}'))[1]::VARCHAR AS INTEGER)
            AS TotalChildren
    ,CAST((xpath('n:NumberChildrenAtHome/text()', Demographics, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/IndividualSurvey}}'))[1]::VARCHAR AS INTEGER)
            AS NumberChildrenAtHome
    ,(xpath('n:Education/text()', Demographics, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/IndividualSurvey}}'))[1]::VARCHAR(30)
            AS Education
    ,(xpath('n:Occupation/text()', Demographics, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/IndividualSurvey}}'))[1]::VARCHAR(30)
            AS Occupation
    ,CAST((xpath('n:HomeOwnerFlag/text()', Demographics, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/IndividualSurvey}}'))[1]::VARCHAR AS BOOLEAN)
            AS HomeOwnerFlag
    ,CAST((xpath('n:NumberCarsOwned/text()', Demographics, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/IndividualSurvey}}'))[1]::VARCHAR AS INTEGER)
            AS NumberCarsOwned
FROM Person.Person
  WHERE Demographics IS NOT NULL;


CREATE VIEW HumanResources.vJobCandidate
AS
SELECT
    JobCandidateID
    ,BusinessEntityID
    ,(xpath('/n:Resume/n:Name/n:Name.Prefix/text()', Resume, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))[1]::varchar(30)
                   AS "Name.Prefix"
    ,(xpath('/n:Resume/n:Name/n:Name.First/text()', Resume, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))[1]::varchar(30)
                   AS "Name.First"
    ,(xpath('/n:Resume/n:Name/n:Name.Middle/text()', Resume, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))[1]::varchar(30)
                   AS "Name.Middle"
    ,(xpath('/n:Resume/n:Name/n:Name.Last/text()', Resume, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))[1]::varchar(30)
                   AS "Name.Last"
    ,(xpath('/n:Resume/n:Name/n:Name.Suffix/text()', Resume, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))[1]::varchar(30)
                   AS "Name.Suffix"
    ,(xpath('/n:Resume/n:Skills/text()', Resume, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))[1]::varchar
                   AS "Skills"
    ,(xpath('n:Address/n:Addr.Type/text()', Resume, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))[1]::varchar(30)
                   AS "Addr.Type"
    ,(xpath('n:Address/n:Addr.Location/n:Location/n:Loc.CountryRegion/text()', Resume, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))[1]::varchar(100)
                   AS "Addr.Loc.CountryRegion"
    ,(xpath('n:Address/n:Addr.Location/n:Location/n:Loc.State/text()', Resume, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))[1]::varchar(100)
                   AS "Addr.Loc.State"
    ,(xpath('n:Address/n:Addr.Location/n:Location/n:Loc.City/text()', Resume, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))[1]::varchar(100)
                   AS "Addr.Loc.City"
    ,(xpath('n:Address/n:Addr.PostalCode/text()', Resume, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))[1]::varchar(20)
                   AS "Addr.PostalCode"
    ,(xpath('/n:Resume/n:EMail/text()', Resume, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))[1]::varchar
                   AS "EMail"
    ,(xpath('/n:Resume/n:WebSite/text()', Resume, '{{n,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))[1]::varchar
                   AS "WebSite"
    ,ModifiedDate
FROM HumanResources.JobCandidate;


-- In this case we UNNEST in order to have multiple previous employments listed for
-- each job candidate.  But things become very brittle when using UNNEST like this,
-- with multiple columns...
-- ... if any of our Employment fragments were missing something, such as randomly a
-- Emp.FunctionCategory is not there, then there will be 0 rows returned.  Each
-- Employment element must contain all 10 sub-elements for this approach to work.
-- (See the Education example below for a better alternate approach!)
CREATE VIEW HumanResources.vJobCandidateEmployment
AS
SELECT
    JobCandidateID
    ,CAST(UNNEST(xpath('/ns:Resume/ns:Employment/ns:Emp.StartDate/text()', Resume, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))::VARCHAR(20) AS DATE)
                                                AS "Emp.StartDate"
    ,CAST(UNNEST(xpath('/ns:Resume/ns:Employment/ns:Emp.EndDate/text()', Resume, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))::VARCHAR(20) AS DATE)
                                                AS "Emp.EndDate"
    ,UNNEST(xpath('/ns:Resume/ns:Employment/ns:Emp.OrgName/text()', Resume, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))::varchar(100)
                                                AS "Emp.OrgName"
    ,UNNEST(xpath('/ns:Resume/ns:Employment/ns:Emp.JobTitle/text()', Resume, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))::varchar(100)
                                                AS "Emp.JobTitle"
    ,UNNEST(xpath('/ns:Resume/ns:Employment/ns:Emp.Responsibility/text()', Resume, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))::varchar
                                                AS "Emp.Responsibility"
    ,UNNEST(xpath('/ns:Resume/ns:Employment/ns:Emp.FunctionCategory/text()', Resume, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))::varchar
                                                AS "Emp.FunctionCategory"
    ,UNNEST(xpath('/ns:Resume/ns:Employment/ns:Emp.IndustryCategory/text()', Resume, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))::varchar
                                                AS "Emp.IndustryCategory"
    ,UNNEST(xpath('/ns:Resume/ns:Employment/ns:Emp.Location/ns:Location/ns:Loc.CountryRegion/text()', Resume, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))::varchar
                                                AS "Emp.Loc.CountryRegion"
    ,UNNEST(xpath('/ns:Resume/ns:Employment/ns:Emp.Location/ns:Location/ns:Loc.State/text()', Resume, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))::varchar
                                                AS "Emp.Loc.State"
    ,UNNEST(xpath('/ns:Resume/ns:Employment/ns:Emp.Location/ns:Location/ns:Loc.City/text()', Resume, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}'))::varchar
                                                AS "Emp.Loc.City"
  FROM HumanResources.JobCandidate;


-- In this data set, not every listed education has a minor.  (OK, actually NONE of them do!)
-- So instead of using multiple UNNEST as above, which would result in 0 rows returned,
-- we just UNNEST once in a derived table, then convert each XML fragment into a document again
-- with one <root> element and a shorter namespace for ns:, and finally just use xpath on
-- all the created documents.
CREATE VIEW HumanResources.vJobCandidateEducation
AS
SELECT
  jc.JobCandidateID
  ,(xpath('/root/ns:Education/ns:Edu.Level/text()', jc.doc, '{{ns,http://adventureworks.com}}'))[1]::varchar(50)
                             AS "Edu.Level"
  ,CAST((xpath('/root/ns:Education/ns:Edu.StartDate/text()', jc.doc, '{{ns,http://adventureworks.com}}'))[1]::VARCHAR(20) AS DATE)
                             AS "Edu.StartDate"
  ,CAST((xpath('/root/ns:Education/ns:Edu.EndDate/text()', jc.doc, '{{ns,http://adventureworks.com}}'))[1]::VARCHAR(20) AS DATE)
                             AS "Edu.EndDate"
  ,(xpath('/root/ns:Education/ns:Edu.Degree/text()', jc.doc, '{{ns,http://adventureworks.com}}'))[1]::varchar(50)
                             AS "Edu.Degree"
  ,(xpath('/root/ns:Education/ns:Edu.Major/text()', jc.doc, '{{ns,http://adventureworks.com}}'))[1]::varchar(50)
                             AS "Edu.Major"
  ,(xpath('/root/ns:Education/ns:Edu.Minor/text()', jc.doc, '{{ns,http://adventureworks.com}}'))[1]::varchar(50)
                             AS "Edu.Minor"
  ,(xpath('/root/ns:Education/ns:Edu.GPA/text()', jc.doc, '{{ns,http://adventureworks.com}}'))[1]::varchar(5)
                             AS "Edu.GPA"
  ,(xpath('/root/ns:Education/ns:Edu.GPAScale/text()', jc.doc, '{{ns,http://adventureworks.com}}'))[1]::varchar(5)
                             AS "Edu.GPAScale"
  ,(xpath('/root/ns:Education/ns:Edu.School/text()', jc.doc, '{{ns,http://adventureworks.com}}'))[1]::varchar(100)
                             AS "Edu.School"
  ,(xpath('/root/ns:Education/ns:Edu.Location/ns:Location/ns:Loc.CountryRegion/text()', jc.doc, '{{ns,http://adventureworks.com}}'))[1]::varchar(100)
                             AS "Edu.Loc.CountryRegion"
  ,(xpath('/root/ns:Education/ns:Edu.Location/ns:Location/ns:Loc.State/text()', jc.doc, '{{ns,http://adventureworks.com}}'))[1]::varchar(100)
                             AS "Edu.Loc.State"
  ,(xpath('/root/ns:Education/ns:Edu.Location/ns:Location/ns:Loc.City/text()', jc.doc, '{{ns,http://adventureworks.com}}'))[1]::varchar(100)
                             AS "Edu.Loc.City"
FROM (SELECT JobCandidateID
    -- Because the underlying XML data used in this example has namespaces defined at the document level,
    -- when we take individual fragments using UNNEST then each fragment has no idea of the namespaces.
    -- So here each fragment gets turned back into its own document with a root element that defines a
    -- simpler thing for "ns" since this will only be used only in the xpath queries above.
    ,('<root xmlns:ns="http://adventureworks.com">' ||
      unnesting.Education::varchar ||
      '</root>')::xml AS doc
  FROM (SELECT JobCandidateID
      ,UNNEST(xpath('/ns:Resume/ns:Education', Resume, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/Resume}}')) AS Education
    FROM HumanResources.JobCandidate) AS unnesting) AS jc;


-- Products and product descriptions by language.
-- We're making this a materialized view so that performance can be better.
CREATE MATERIALIZED VIEW Production.vProductAndDescription
AS
SELECT
    p.ProductID
    ,p.Name
    ,pm.Name AS ProductModel
    ,pmx.CultureID
    ,pd.Description
FROM Production.Product p
    INNER JOIN Production.ProductModel pm
    ON p.ProductModelID = pm.ProductModelID
    INNER JOIN Production.ProductModelProductDescriptionCulture pmx
    ON pm.ProductModelID = pmx.ProductModelID
    INNER JOIN Production.ProductDescription pd
    ON pmx.ProductDescriptionID = pd.ProductDescriptionID;

-- Index the vProductAndDescription view
CREATE UNIQUE INDEX IX_vProductAndDescription ON Production.vProductAndDescription(CultureID, ProductID);
CLUSTER Production.vProductAndDescription USING IX_vProductAndDescription;
-- Note that with a materialized view, changes to the underlying tables will
-- not change the contents of the view.  In order to maintain the index, if there
-- are changes to any of the 4 tables then you would need to run:
--   REFRESH MATERIALIZED VIEW Production.vProductAndDescription;


CREATE VIEW Production.vProductModelCatalogDescription
AS
SELECT
  ProductModelID
  ,Name
  ,(xpath('/p1:ProductDescription/p1:Summary/html:p/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription},{html,http://www.w3.org/1999/xhtml}}'))[1]::varchar
                                 AS "Summary"
  ,(xpath('/p1:ProductDescription/p1:Manufacturer/p1:Name/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription}}' ))[1]::varchar
                                  AS Manufacturer
  ,(xpath('/p1:ProductDescription/p1:Manufacturer/p1:Copyright/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription}}' ))[1]::varchar(30)
                                                  AS Copyright
  ,(xpath('/p1:ProductDescription/p1:Manufacturer/p1:ProductURL/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription}}' ))[1]::varchar(256)
                                                  AS ProductURL
  ,(xpath('/p1:ProductDescription/p1:Features/wm:Warranty/wm:WarrantyPeriod/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription},{wm,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelWarrAndMain}}' ))[1]::varchar(256)
                                                          AS WarrantyPeriod
  ,(xpath('/p1:ProductDescription/p1:Features/wm:Warranty/wm:Description/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription},{wm,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelWarrAndMain}}' ))[1]::varchar(256)
                                                          AS WarrantyDescription
  ,(xpath('/p1:ProductDescription/p1:Features/wm:Maintenance/wm:NoOfYears/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription},{wm,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelWarrAndMain}}' ))[1]::varchar(256)
                                                             AS NoOfYears
  ,(xpath('/p1:ProductDescription/p1:Features/wm:Maintenance/wm:Description/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription},{wm,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelWarrAndMain}}' ))[1]::varchar(256)
                                                             AS MaintenanceDescription
  ,(xpath('/p1:ProductDescription/p1:Features/wf:wheel/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription},{wf,http://www.adventure-works.com/schemas/OtherFeatures}}'))[1]::varchar(256)
                                              AS Wheel
  ,(xpath('/p1:ProductDescription/p1:Features/wf:saddle/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription},{wf,http://www.adventure-works.com/schemas/OtherFeatures}}'))[1]::varchar(256)
                                              AS Saddle
  ,(xpath('/p1:ProductDescription/p1:Features/wf:pedal/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription},{wf,http://www.adventure-works.com/schemas/OtherFeatures}}'))[1]::varchar(256)
                                              AS Pedal
  ,(xpath('/p1:ProductDescription/p1:Features/wf:BikeFrame/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription},{wf,http://www.adventure-works.com/schemas/OtherFeatures}}'))[1]::varchar
                                              AS BikeFrame
  ,(xpath('/p1:ProductDescription/p1:Features/wf:crankset/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription},{wf,http://www.adventure-works.com/schemas/OtherFeatures}}'))[1]::varchar(256)
                                              AS Crankset
  ,(xpath('/p1:ProductDescription/p1:Picture/p1:Angle/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription}}' ))[1]::varchar(256)
                                             AS PictureAngle
  ,(xpath('/p1:ProductDescription/p1:Picture/p1:Size/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription}}' ))[1]::varchar(256)
                                             AS PictureSize
  ,(xpath('/p1:ProductDescription/p1:Picture/p1:ProductPhotoID/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription}}' ))[1]::varchar(256)
                                             AS ProductPhotoID
  ,(xpath('/p1:ProductDescription/p1:Specifications/Material/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription}}' ))[1]::varchar(256)
                                                 AS Material
  ,(xpath('/p1:ProductDescription/p1:Specifications/Color/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription}}' ))[1]::varchar(256)
                                                 AS Color
  ,(xpath('/p1:ProductDescription/p1:Specifications/ProductLine/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription}}' ))[1]::varchar(256)
                                                 AS ProductLine
  ,(xpath('/p1:ProductDescription/p1:Specifications/Style/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription}}' ))[1]::varchar(256)
                                                 AS Style
  ,(xpath('/p1:ProductDescription/p1:Specifications/RiderExperience/text()', CatalogDescription, '{{p1,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelDescription}}' ))[1]::varchar(1024)
                                                 AS RiderExperience
  ,rowguid
  ,ModifiedDate
FROM Production.ProductModel
WHERE CatalogDescription IS NOT NULL;


-- Instructions have many locations, and locations have many steps
CREATE VIEW Production.vProductModelInstructions
AS
SELECT
    pm.ProductModelID
    ,pm.Name
    -- Access the overall Instructions xml brought through from %line 2938 and %line 2943
    ,(xpath('/ns:root/text()', pm.Instructions, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelManuInstructions}}'))[1]::varchar AS Instructions
    -- Bring out information about the location, broken out in %line 2945
    ,CAST((xpath('@LocationID', pm.MfgInstructions))[1]::varchar AS INTEGER) AS "LocationID"
    ,CAST((xpath('@SetupHours', pm.MfgInstructions))[1]::varchar AS DECIMAL(9, 4)) AS "SetupHours"
    ,CAST((xpath('@MachineHours', pm.MfgInstructions))[1]::varchar AS DECIMAL(9, 4)) AS "MachineHours"
    ,CAST((xpath('@LaborHours', pm.MfgInstructions))[1]::varchar AS DECIMAL(9, 4)) AS "LaborHours"
    ,CAST((xpath('@LotSize', pm.MfgInstructions))[1]::varchar AS INTEGER) AS "LotSize"
    -- Show specific detail about each step broken out in %line 2940
    ,(xpath('/step/text()', pm.Step))[1]::varchar(1024) AS "Step"
    ,pm.rowguid
    ,pm.ModifiedDate
FROM (SELECT locations.ProductModelID, locations.Name, locations.rowguid, locations.ModifiedDate
    ,locations.Instructions, locations.MfgInstructions
    -- Further break out the location information from the inner query below into individual steps
    ,UNNEST(xpath('step', locations.MfgInstructions)) AS Step
  FROM (SELECT
      -- Just pass these through so they can be referenced at the outermost query
      ProductModelID, Name, rowguid, ModifiedDate, Instructions
      -- And also break out Instructions into individual locations to pass up to the middle query
      ,UNNEST(xpath('/ns:root/ns:Location', Instructions, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/ProductModelManuInstructions}}')) AS MfgInstructions
    FROM Production.ProductModel) AS locations) AS pm;


CREATE VIEW Sales.vSalesPerson
AS
SELECT
    s.BusinessEntityID
    ,p.Title
    ,p.FirstName
    ,p.MiddleName
    ,p.LastName
    ,p.Suffix
    ,e.JobTitle
    ,pp.PhoneNumber
    ,pnt.Name AS PhoneNumberType
    ,ea.EmailAddress
    ,p.EmailPromotion
    ,a.AddressLine1
    ,a.AddressLine2
    ,a.City
    ,sp.Name AS StateProvinceName
    ,a.PostalCode
    ,cr.Name AS CountryRegionName
    ,st.Name AS TerritoryName
    ,st.Group AS TerritoryGroup
    ,s.SalesQuota
    ,s.SalesYTD
    ,s.SalesLastYear
FROM Sales.SalesPerson s
  INNER JOIN HumanResources.Employee e
    ON e.BusinessEntityID = s.BusinessEntityID
  INNER JOIN Person.Person p
    ON p.BusinessEntityID = s.BusinessEntityID
  INNER JOIN Person.BusinessEntityAddress bea
    ON bea.BusinessEntityID = s.BusinessEntityID
  INNER JOIN Person.Address a
    ON a.AddressID = bea.AddressID
  INNER JOIN Person.StateProvince sp
    ON sp.StateProvinceID = a.StateProvinceID
  INNER JOIN Person.CountryRegion cr
    ON cr.CountryRegionCode = sp.CountryRegionCode
  LEFT OUTER JOIN Sales.SalesTerritory st
    ON st.TerritoryID = s.TerritoryID
  LEFT OUTER JOIN Person.EmailAddress ea
    ON ea.BusinessEntityID = p.BusinessEntityID
  LEFT OUTER JOIN Person.PersonPhone pp
    ON pp.BusinessEntityID = p.BusinessEntityID
  LEFT OUTER JOIN Person.PhoneNumberType pnt
    ON pnt.PhoneNumberTypeID = pp.PhoneNumberTypeID;


-- This view provides the aggregated data that gets used in the PIVOTed view below
CREATE VIEW Sales.vSalesPersonSalesByFiscalYearsData
AS
-- Of the 56 possible combinations of one of the 14 SalesPersons selling across one of
-- 4 FiscalYears, here we end up with 48 rows of aggregated data (since some sales people
-- were hired and started working in FY2012 or FY2013).
SELECT granular.SalesPersonID, granular.FullName, granular.JobTitle, granular.SalesTerritory, SUM(granular.SubTotal) AS SalesTotal, granular.FiscalYear
FROM
-- Brings back 3703 rows of data -- there are 3806 total sales done by a SalesPerson,
-- of which 103 do not have any sales territory.  This is fed into the outer GROUP BY
-- which results in 48 aggregated rows of sales data.
  (SELECT
      soh.SalesPersonID
      ,p.FirstName || ' ' || COALESCE(p.MiddleName || ' ', '') || p.LastName AS FullName
      ,e.JobTitle
      ,st.Name AS SalesTerritory
      ,soh.SubTotal
      ,EXTRACT(YEAR FROM soh.OrderDate + '6 months'::interval) AS FiscalYear
  FROM Sales.SalesPerson sp
    INNER JOIN Sales.SalesOrderHeader soh
      ON sp.BusinessEntityID = soh.SalesPersonID
    INNER JOIN Sales.SalesTerritory st
      ON sp.TerritoryID = st.TerritoryID
    INNER JOIN HumanResources.Employee e
      ON soh.SalesPersonID = e.BusinessEntityID
    INNER JOIN Person.Person p
      ON p.BusinessEntityID = sp.BusinessEntityID
  ) AS granular
GROUP BY granular.SalesPersonID, granular.FullName, granular.JobTitle, granular.SalesTerritory, granular.FiscalYear;

-- Note that this PIVOT query originally refered to years 2002-2004, which jived with
-- earlier versions of the AdventureWorks data.  Somewhere along the way all the dates
-- were cranked forward by exactly a decade, but this view wasn't updated, effectively
-- breaking it.  The hard-coded fiscal years below fix this issue.

-- Current sales data ranges from May 31, 2011 through June 30, 2014, so there's one
-- month of fiscal year 2011 data, but mostly FY 2012 through 2014.

-- This query properly shows no data for three of our sales people in 2012,
-- as they were hired during FY 2013.
CREATE VIEW Sales.vSalesPersonSalesByFiscalYears
AS
SELECT * FROM crosstab(
'SELECT
    SalesPersonID
    ,FullName
    ,JobTitle
    ,SalesTerritory
    ,FiscalYear
    ,SalesTotal
FROM Sales.vSalesPersonSalesByFiscalYearsData
ORDER BY 2,4'
-- This set of fiscal years could have dynamically come from a SELECT DISTINCT,
-- but we wanted to omit 2011 and also ...
,$$SELECT unnest('{2012,2013,2014}'::text[])$$)
-- ... still the FiscalYear values have to be hard-coded here.
AS SalesTotal ("SalesPersonID" integer, "FullName" text, "JobTitle" text, "SalesTerritory" text,
 "2012" DECIMAL(12, 4), "2013" DECIMAL(12, 4), "2014" DECIMAL(12, 4));


CREATE MATERIALIZED VIEW Person.vStateProvinceCountryRegion
AS
SELECT
    sp.StateProvinceID
    ,sp.StateProvinceCode
    ,sp.IsOnlyStateProvinceFlag
    ,sp.Name AS StateProvinceName
    ,sp.TerritoryID
    ,cr.CountryRegionCode
    ,cr.Name AS CountryRegionName
FROM Person.StateProvince sp
    INNER JOIN Person.CountryRegion cr
    ON sp.CountryRegionCode = cr.CountryRegionCode;

CREATE UNIQUE INDEX IX_vStateProvinceCountryRegion ON Person.vStateProvinceCountryRegion(StateProvinceID, CountryRegionCode);
CLUSTER Person.vStateProvinceCountryRegion USING IX_vStateProvinceCountryRegion;
-- If there are changes to either of these tables, this should be run to update the view:
--   REFRESH MATERIALIZED VIEW Production.vStateProvinceCountryRegion;


CREATE VIEW Sales.vStoreWithDemographics
AS
SELECT
    BusinessEntityID
    ,Name
    ,CAST(UNNEST(xpath('/ns:StoreSurvey/ns:AnnualSales/text()', Demographics, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/StoreSurvey}}'))::varchar AS money)
                                       AS "AnnualSales"
    ,CAST(UNNEST(xpath('/ns:StoreSurvey/ns:AnnualRevenue/text()', Demographics, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/StoreSurvey}}'))::varchar AS money)
                                       AS "AnnualRevenue"
    ,UNNEST(xpath('/ns:StoreSurvey/ns:BankName/text()', Demographics, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/StoreSurvey}}'))::varchar(50)
                                  AS "BankName"
    ,UNNEST(xpath('/ns:StoreSurvey/ns:BusinessType/text()', Demographics, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/StoreSurvey}}'))::varchar(5)
                                  AS "BusinessType"
    ,CAST(UNNEST(xpath('/ns:StoreSurvey/ns:YearOpened/text()', Demographics, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/StoreSurvey}}'))::varchar AS integer)
                                       AS "YearOpened"
    ,UNNEST(xpath('/ns:StoreSurvey/ns:Specialty/text()', Demographics, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/StoreSurvey}}'))::varchar(50)
                                  AS "Specialty"
    ,CAST(UNNEST(xpath('/ns:StoreSurvey/ns:SquareFeet/text()', Demographics, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/StoreSurvey}}'))::varchar AS integer)
                                       AS "SquareFeet"
    ,UNNEST(xpath('/ns:StoreSurvey/ns:Brands/text()', Demographics, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/StoreSurvey}}'))::varchar(30)
                                  AS "Brands"
    ,UNNEST(xpath('/ns:StoreSurvey/ns:Internet/text()', Demographics, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/StoreSurvey}}'))::varchar(30)
                                  AS "Internet"
    ,CAST(UNNEST(xpath('/ns:StoreSurvey/ns:NumberEmployees/text()', Demographics, '{{ns,http://schemas.microsoft.com/sqlserver/2004/07/adventure-works/StoreSurvey}}'))::varchar AS integer)
                                       AS "NumberEmployees"
FROM Sales.Store;


CREATE VIEW Sales.vStoreWithContacts
AS
SELECT
    s.BusinessEntityID
    ,s.Name
    ,ct.Name AS ContactType
    ,p.Title
    ,p.FirstName
    ,p.MiddleName
    ,p.LastName
    ,p.Suffix
    ,pp.PhoneNumber
    ,pnt.Name AS PhoneNumberType
    ,ea.EmailAddress
    ,p.EmailPromotion
FROM Sales.Store s
  INNER JOIN Person.BusinessEntityContact bec
    ON bec.BusinessEntityID = s.BusinessEntityID
  INNER JOIN Person.ContactType ct
    ON ct.ContactTypeID = bec.ContactTypeID
  INNER JOIN Person.Person p
    ON p.BusinessEntityID = bec.PersonID
  LEFT OUTER JOIN Person.EmailAddress ea
    ON ea.BusinessEntityID = p.BusinessEntityID
  LEFT OUTER JOIN Person.PersonPhone pp
    ON pp.BusinessEntityID = p.BusinessEntityID
  LEFT OUTER JOIN Person.PhoneNumberType pnt
    ON pnt.PhoneNumberTypeID = pp.PhoneNumberTypeID;


CREATE VIEW Sales.vStoreWithAddresses
AS
SELECT
    s.BusinessEntityID
    ,s.Name
    ,at.Name AS AddressType
    ,a.AddressLine1
    ,a.AddressLine2
    ,a.City
    ,sp.Name AS StateProvinceName
    ,a.PostalCode
    ,cr.Name AS CountryRegionName
FROM Sales.Store s
  INNER JOIN Person.BusinessEntityAddress bea
    ON bea.BusinessEntityID = s.BusinessEntityID
  INNER JOIN Person.Address a
    ON a.AddressID = bea.AddressID
  INNER JOIN Person.StateProvince sp
    ON sp.StateProvinceID = a.StateProvinceID
  INNER JOIN Person.CountryRegion cr
    ON cr.CountryRegionCode = sp.CountryRegionCode
  INNER JOIN Person.AddressType at
    ON at.AddressTypeID = bea.AddressTypeID;


CREATE VIEW Purchasing.vVendorWithContacts
AS
SELECT
    v.BusinessEntityID
    ,v.Name
    ,ct.Name AS ContactType
    ,p.Title
    ,p.FirstName
    ,p.MiddleName
    ,p.LastName
    ,p.Suffix
    ,pp.PhoneNumber
    ,pnt.Name AS PhoneNumberType
    ,ea.EmailAddress
    ,p.EmailPromotion
FROM Purchasing.Vendor v
  INNER JOIN Person.BusinessEntityContact bec
    ON bec.BusinessEntityID = v.BusinessEntityID
  INNER JOIN Person.ContactType ct
    ON ct.ContactTypeID = bec.ContactTypeID
  INNER JOIN Person.Person p
    ON p.BusinessEntityID = bec.PersonID
  LEFT OUTER JOIN Person.EmailAddress ea
    ON ea.BusinessEntityID = p.BusinessEntityID
  LEFT OUTER JOIN Person.PersonPhone pp
    ON pp.BusinessEntityID = p.BusinessEntityID
  LEFT OUTER JOIN Person.PhoneNumberType pnt
    ON pnt.PhoneNumberTypeID = pp.PhoneNumberTypeID;


CREATE VIEW Purchasing.vVendorWithAddresses
AS
SELECT
    v.BusinessEntityID
    ,v.Name
    ,at.Name AS AddressType
    ,a.AddressLine1
    ,a.AddressLine2
    ,a.City
    ,sp.Name AS StateProvinceName
    ,a.PostalCode
    ,cr.Name AS CountryRegionName
FROM Purchasing.Vendor v
  INNER JOIN Person.BusinessEntityAddress bea
    ON bea.BusinessEntityID = v.BusinessEntityID
  INNER JOIN Person.Address a
    ON a.AddressID = bea.AddressID
  INNER JOIN Person.StateProvince sp
    ON sp.StateProvinceID = a.StateProvinceID
  INNER JOIN Person.CountryRegion cr
    ON cr.CountryRegionCode = sp.CountryRegionCode
  INNER JOIN Person.AddressType at
    ON at.AddressTypeID = bea.AddressTypeID;


-- Convenience views

CREATE SCHEMA pe
  CREATE VIEW a AS SELECT addressid AS id, * FROM person.address
  CREATE VIEW at AS SELECT addresstypeid AS id, * FROM person.addresstype
  CREATE VIEW be AS SELECT businessentityid AS id, * FROM person.businessentity
  CREATE VIEW bea AS SELECT businessentityid AS id, * FROM person.businessentityaddress
  CREATE VIEW bec AS SELECT businessentityid AS id, * FROM person.businessentitycontact
  CREATE VIEW ct AS SELECT contacttypeid AS id, * FROM person.contacttype
  CREATE VIEW cr AS SELECT * FROM person.countryregion
  CREATE VIEW e AS SELECT emailaddressid AS id, * FROM person.emailaddress
  CREATE VIEW pa AS SELECT businessentityid AS id, * FROM person.password
  CREATE VIEW p AS SELECT businessentityid AS id, * FROM person.person
  CREATE VIEW pp AS SELECT businessentityid AS id, * FROM person.personphone
  CREATE VIEW pnt AS SELECT phonenumbertypeid AS id, * FROM person.phonenumbertype
  CREATE VIEW sp AS SELECT stateprovinceid AS id, * FROM person.stateprovince
;
CREATE SCHEMA hr
  CREATE VIEW d AS SELECT departmentid AS id, * FROM humanresources.department
  CREATE VIEW e AS SELECT businessentityid AS id, * FROM humanresources.employee
  CREATE VIEW edh AS SELECT businessentityid AS id, * FROM humanresources.employeedepartmenthistory
  CREATE VIEW eph AS SELECT businessentityid AS id, * FROM humanresources.employeepayhistory
  CREATE VIEW jc AS SELECT jobcandidateid AS id, * FROM humanresources.jobcandidate
  CREATE VIEW s AS SELECT shiftid AS id, * FROM humanresources.shift
;
CREATE SCHEMA pr
  CREATE VIEW bom AS SELECT billofmaterialsid AS id, * FROM production.billofmaterials
  CREATE VIEW c AS SELECT cultureid AS id, * FROM production.culture
  CREATE VIEW d AS SELECT * FROM production.document
  CREATE VIEW i AS SELECT illustrationid AS id, * FROM production.illustration
  CREATE VIEW l AS SELECT locationid AS id, * FROM production.location
  CREATE VIEW p AS SELECT productid AS id, * FROM production.product
  CREATE VIEW pc AS SELECT productcategoryid AS id, * FROM production.productcategory
  CREATE VIEW pch AS SELECT productid AS id, * FROM production.productcosthistory
  CREATE VIEW pd AS SELECT productdescriptionid AS id, * FROM production.productdescription
  CREATE VIEW pdoc AS SELECT productid AS id, * FROM production.productdocument
  CREATE VIEW pi AS SELECT productid AS id, * FROM production.productinventory
  CREATE VIEW plph AS SELECT productid AS id, * FROM production.productlistpricehistory
  CREATE VIEW pm AS SELECT productmodelid AS id, * FROM production.productmodel
  CREATE VIEW pmi AS SELECT * FROM production.productmodelillustration
  CREATE VIEW pmpdc AS SELECT * FROM production.productmodelproductdescriptionculture
  CREATE VIEW pp AS SELECT productphotoid AS id, * FROM production.productphoto
  CREATE VIEW ppp AS SELECT * FROM production.productproductphoto
  CREATE VIEW pr AS SELECT productreviewid AS id, * FROM production.productreview
  CREATE VIEW psc AS SELECT productsubcategoryid AS id, * FROM production.productsubcategory
  CREATE VIEW sr AS SELECT scrapreasonid AS id, * FROM production.scrapreason
  CREATE VIEW th AS SELECT transactionid AS id, * FROM production.transactionhistory
  CREATE VIEW tha AS SELECT transactionid AS id, * FROM production.transactionhistoryarchive
  CREATE VIEW um AS SELECT unitmeasurecode AS id, * FROM production.unitmeasure
  CREATE VIEW w AS SELECT workorderid AS id, * FROM production.workorder
  CREATE VIEW wr AS SELECT workorderid AS id, * FROM production.workorderrouting
;
CREATE SCHEMA pu
  CREATE VIEW pv AS SELECT productid AS id, * FROM purchasing.productvendor
  CREATE VIEW pod AS SELECT purchaseorderdetailid AS id, * FROM purchasing.purchaseorderdetail
  CREATE VIEW poh AS SELECT purchaseorderid AS id, * FROM purchasing.purchaseorderheader
  CREATE VIEW sm AS SELECT shipmethodid AS id, * FROM purchasing.shipmethod
  CREATE VIEW v AS SELECT businessentityid AS id, * FROM purchasing.vendor
;
CREATE SCHEMA sa
  CREATE VIEW crc AS SELECT * FROM sales.countryregioncurrency
  CREATE VIEW cc AS SELECT creditcardid AS id, * FROM sales.creditcard
  CREATE VIEW cu AS SELECT currencycode AS id, * FROM sales.currency
  CREATE VIEW cr AS SELECT * FROM sales.currencyrate
  CREATE VIEW c AS SELECT customerid AS id, * FROM sales.customer
  CREATE VIEW pcc AS SELECT businessentityid AS id, * FROM sales.personcreditcard
  CREATE VIEW sod AS SELECT salesorderdetailid AS id, * FROM sales.salesorderdetail
  CREATE VIEW soh AS SELECT salesorderid AS id, * FROM sales.salesorderheader
  CREATE VIEW sohsr AS SELECT * FROM sales.salesorderheadersalesreason
  CREATE VIEW sp AS SELECT businessentityid AS id, * FROM sales.salesperson
  CREATE VIEW spqh AS SELECT businessentityid AS id, * FROM sales.salespersonquotahistory
  CREATE VIEW sr AS SELECT salesreasonid AS id, * FROM sales.salesreason
  CREATE VIEW tr AS SELECT salestaxrateid AS id, * FROM sales.salestaxrate
  CREATE VIEW st AS SELECT territoryid AS id, * FROM sales.salesterritory
  CREATE VIEW sth AS SELECT territoryid AS id, * FROM sales.salesterritoryhistory
  CREATE VIEW sci AS SELECT shoppingcartitemid AS id, * FROM sales.shoppingcartitem
  CREATE VIEW so AS SELECT specialofferid AS id, * FROM sales.specialoffer
  CREATE VIEW sop AS SELECT specialofferid AS id, * FROM sales.specialofferproduct
  CREATE VIEW s AS SELECT businessentityid AS id, * FROM sales.store
;

-- Ensure that Perry Skountrianos' change for Türkiye is implemented properly if it was intended
-- https://github.com/microsoft/sql-server-samples/commit/cca0f1920e3bec5b9cef97e1fdc32b6883526581
-- Fix any messed up one if such a thing exists and is not proper unicode
UPDATE person.countryregion SET name='T' || U&'\00FC' || 'rkiye' WHERE name='Türkiye';
-- Optionally you can uncomment this to update "Turkey" to the unicode-appropriate rendition of "Türkiye"
-- UPDATE person.countryregion SET name='T' || U&'\00FC' || 'rkiye' WHERE name='Turkey';

-- If you intend to use this data with The Brick (or some other Rails project) then you may want
-- to rename the "class" column in Production.Product so it does not interfere with Ruby's reserved
-- keyword "class":
-- ALTER TABLE production.product RENAME COLUMN class TO class_;
\pset tuples_only off



-- 805 rows in BusinessEntity but not in Person
-- SELECT be.businessentityid FROM person.businessentity AS be LEFT OUTER JOIN person.person AS p ON be.businessentityid = p.businessentityid WHERE p.businessentityid IS NULL;

-- All the tables in Adventureworks:
-- (Did you know that \dt can filter schema and table names using RegEx?)
\dt (humanresources|person|production|purchasing|sales).*

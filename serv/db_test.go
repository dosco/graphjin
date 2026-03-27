package serv

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"

	"github.com/dosco/graphjin/core/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitMssql_BasicConnectionString(t *testing.T) {
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:     "mssql",
				Host:     "localhost",
				Port:     1433,
				User:     "sa",
				Password: "mypassword",
				DBName:   "testdb",
			},
		},
	}

	dc, err := initMssql(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Equal(t, "sqlserver", dc.driverName)
	assert.Equal(t, "sqlserver://sa:mypassword@localhost:1433?database=testdb", dc.connString)
}

func TestInitMssql_EncryptDisable(t *testing.T) {
	boolFalse := false
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:     "mssql",
				Host:     "localhost",
				Port:     1433,
				User:     "sa",
				Password: "pass",
				DBName:   "testdb",
				Encrypt:  &boolFalse,
			},
		},
	}

	dc, err := initMssql(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Contains(t, dc.connString, "encrypt=disable")
	assert.Contains(t, dc.connString, "database=testdb")
}

func TestInitMssql_EncryptTrue(t *testing.T) {
	boolTrue := true
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:     "mssql",
				Host:     "localhost",
				Port:     1433,
				User:     "sa",
				Password: "pass",
				Encrypt:  &boolTrue,
			},
		},
	}

	dc, err := initMssql(conf, false, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Contains(t, dc.connString, "encrypt=true")
}

func TestInitMssql_TrustServerCertificate(t *testing.T) {
	boolFalse := false
	boolTrue := true
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:                   "mssql",
				Host:                   "localhost",
				Port:                   1433,
				User:                   "sa",
				Password:               "pass",
				DBName:                 "testdb",
				Encrypt:                &boolFalse,
				TrustServerCertificate: &boolTrue,
			},
		},
	}

	dc, err := initMssql(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Contains(t, dc.connString, "encrypt=disable")
	assert.Contains(t, dc.connString, "trustservercertificate=true")
}

func TestInitMssql_SpecialCharsInPassword(t *testing.T) {
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:     "mssql",
				Host:     "localhost",
				Port:     1433,
				User:     "sa",
				Password: "GraphJin!Passw0rd",
				DBName:   "testdb",
			},
		},
	}

	dc, err := initMssql(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Contains(t, dc.connString, "GraphJin%21Passw0rd")
	assert.NotContains(t, dc.connString, "GraphJin!Passw0rd")
}

func TestInitMssql_ConnStringUsedAsIs(t *testing.T) {
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:       "mssql",
				ConnString: "sqlserver://custom:conn@host:1433",
				DBName:     "testdb",
			},
		},
	}

	dc, err := initMssql(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Equal(t, "sqlserver://custom:conn@host:1433?database=testdb", dc.connString)
}

func TestInitMssql_DefaultPort(t *testing.T) {
	tests := []struct {
		name string
		port uint16
	}{
		{"port zero defaults to 1433", 0},
		{"postgres port on non-postgres defaults to 1433", 5432},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf := &Config{
				Serv: Serv{
					DB: Database{
						Type:     "mssql",
						Host:     "localhost",
						Port:     tt.port,
						User:     "sa",
						Password: "pass",
					},
				},
			}

			dc, err := initMssql(conf, false, false, core.NewOsFS(""))
			require.NoError(t, err)
			assert.Contains(t, dc.connString, "localhost:1433")
		})
	}
}

func TestInitMssql_AllOptions(t *testing.T) {
	boolFalse := false
	boolTrue := true
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:                   "mssql",
				Host:                   "mssqlhost",
				Port:                   1433,
				User:                   "sa",
				Password:               "GraphJin!Passw0rd",
				DBName:                 "testdb",
				Encrypt:                &boolFalse,
				TrustServerCertificate: &boolTrue,
			},
		},
	}

	dc, err := initMssql(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	expected := "sqlserver://sa:GraphJin%21Passw0rd@mssqlhost:1433?database=testdb&encrypt=disable&trustservercertificate=true"
	assert.Equal(t, expected, dc.connString)
}

func TestInitSnowflake_RequiresConnectionString(t *testing.T) {
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type: "snowflake",
			},
		},
	}

	_, err := initSnowflake(conf, true, false, core.NewOsFS(""))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "snowflake requires connection_string")
}

func TestInitSnowflake_UsesConnectionString(t *testing.T) {
	conn := "user:pass@localhost:8080/test_db/public?account=test&protocol=http&warehouse=dummy"
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:       "snowflake",
				ConnString: conn,
			},
		},
	}

	dc, err := initSnowflake(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Equal(t, "snowflake", dc.driverName)
	assert.Equal(t, conn, dc.connString)
}

func TestInitDBDriver_DBTypeFallbackToDatabaseType(t *testing.T) {
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:       "snowflake",
				ConnString: "user:pass@localhost:8080/test_db/public?account=test&protocol=http&warehouse=dummy",
			},
		},
	}

	dc, err := initDBDriver(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Equal(t, "snowflake", conf.DBType)
	assert.Equal(t, "snowflake", dc.driverName)
}

// generateTestRSAKeyPEM generates a PKCS#8 PEM-encoded RSA private key for testing.
func generateTestRSAKeyPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func TestLoadSnowflakePrivateKey_ValidPKCS8(t *testing.T) {
	pemData := generateTestRSAKeyPEM(t)
	key, err := loadSnowflakePrivateKey(pemData, "")
	require.NoError(t, err)
	assert.NotNil(t, key)
	assert.Equal(t, 2048, key.N.BitLen())
}

func TestLoadSnowflakePrivateKey_InvalidPEM(t *testing.T) {
	_, err := loadSnowflakePrivateKey([]byte("not valid pem"), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no PEM block found")
}

func TestLoadSnowflakePrivateKey_NonRSAKey(t *testing.T) {
	ecKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(ecKey)
	require.NoError(t, err)
	pemData := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})

	_, err = loadSnowflakePrivateKey(pemData, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not RSA")
}

func TestLoadSnowflakePrivateKey_PKCS1Rejected(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	// PKCS#1 format — should be rejected
	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	_, err = loadSnowflakePrivateKey(pemData, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PKCS#8")
}

func TestInitSnowflake_KeyPairReturnsConnector(t *testing.T) {
	pemData := generateTestRSAKeyPEM(t)
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:          "snowflake",
				ConnString:    "user:pass@localhost:8080/test_db/public?account=test&protocol=http&warehouse=dummy",
				PrivateKeyPEM: string(pemData),
			},
		},
	}

	dc, err := initSnowflake(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.NotNil(t, dc.connector, "key pair auth should return a connector")
	assert.Empty(t, dc.connString, "key pair auth should not set connString")
	assert.Empty(t, dc.driverName, "key pair auth should not set driverName")
}

func TestInitSnowflake_PlainDSNFallback(t *testing.T) {
	conn := "user:pass@localhost:8080/test_db/public?account=test&protocol=http&warehouse=dummy"
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:       "snowflake",
				ConnString: conn,
				// No PrivateKeyPath or PrivateKeyPEM
			},
		},
	}

	dc, err := initSnowflake(conf, true, false, core.NewOsFS(""))
	require.NoError(t, err)
	assert.Equal(t, "snowflake", dc.driverName)
	assert.Equal(t, conn, dc.connString)
	assert.Nil(t, dc.connector, "plain DSN should not use connector")
}

func TestInitSnowflake_InvalidKeyPEM(t *testing.T) {
	conf := &Config{
		Serv: Serv{
			DB: Database{
				Type:          "snowflake",
				ConnString:    "user:pass@localhost:8080/test_db/public?account=test&protocol=http&warehouse=dummy",
				PrivateKeyPEM: "not-a-pem",
			},
		},
	}

	_, err := initSnowflake(conf, true, false, core.NewOsFS(""))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no PEM block found")
}

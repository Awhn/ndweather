package config
import("testing";"os")
func TestLoadValidation(t *testing.T){t.Setenv("INGEST_MODE","http");t.Setenv("INGEST_TOKEN","short");if _,e:=Load();e==nil{t.Fatal("expected token validation")};t.Setenv("INGEST_TOKEN","0123456789abcdef");t.Setenv("PORT","bad");if _,e:=Load();e==nil{t.Fatal("expected port validation")};os.Unsetenv("PORT")}

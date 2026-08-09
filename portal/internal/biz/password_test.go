package biz

import "testing"

func TestHashPassword_CheckPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !CheckPassword(hash, "correct-horse-battery-staple") {
		t.Fatal("CheckPassword should succeed for correct password")
	}
}

func TestCheckPassword_wrongPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if CheckPassword(hash, "wrong-password") {
		t.Fatal("CheckPassword should fail for wrong password")
	}
}

func TestHashPassword_emptyPassword(t *testing.T) {
	_, err := HashPassword("")
	if err == nil {
		t.Fatal("HashPassword should error on empty password")
	}
}

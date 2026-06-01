package modem

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/pbkdf2"
)

// aesEncrypt chiffre un texte en AES-256-CBC style OpenSSL/CryptoJS
// Sortie : base64("Salted__" + salt[8] + ciphertext)
func aesEncrypt(text, key string) (string, error) {
	// 1. Salt aléatoire 8 bytes
	salt := make([]byte, 8)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	// 2. PBKDF2(key, salt, 50 iter, 48 bytes, SHA256) → 32 clé + 16 IV
	derived := pbkdf2.Key([]byte(key), salt, 50, 48, sha256.New)
	aesKey := derived[:32]
	iv := derived[32:48]

	// 3. AES-256-CBC + PKCS7
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", err
	}

	padded := pkcs7Pad([]byte(text), aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)

	// 4. "Salted__" + salt + ciphertext → base64
	payload := append([]byte("Salted__"), salt...)
	payload = append(payload, ciphertext...)
	return base64.StdEncoding.EncodeToString(payload), nil
}

// aesDecrypt déchiffre un texte chiffré par aesEncrypt
func aesDecrypt(encryptedB64, key string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encryptedB64)
	if err != nil {
		return "", fmt.Errorf("base64 decode : %w", err)
	}

	if len(data) < 16 || string(data[:8]) != "Salted__" {
		return "", fmt.Errorf("format invalide : header Salted__ manquant")
	}

	salt := data[8:16]
	ciphertext := data[16:]

	derived := pbkdf2.Key([]byte(key), salt, 50, 48, sha256.New)
	aesKey := derived[:32]
	iv := derived[32:48]

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", err
	}

	if len(ciphertext)%aes.BlockSize != 0 {
		return "", fmt.Errorf("ciphertext non aligné")
	}

	plain := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)

	unpadded, err := pkcs7Unpad(plain)
	if err != nil {
		return "", err
	}

	return string(unpadded), nil
}

// computeHmac calcule HMAC-SHA256(text, key) → hex string
func computeHmac(text, key string) string {
	h := hmac.New(sha256.New, []byte(key))
	h.Write([]byte(text))
	return hex.EncodeToString(h.Sum(nil))
}

// pkcs7Pad ajoute le padding PKCS7
func pkcs7Pad(data []byte, blockSize int) []byte {
	pad := blockSize - len(data)%blockSize
	return append(data, bytes.Repeat([]byte{byte(pad)}, pad)...)
}

// pkcs7Unpad retire le padding PKCS7
func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("données vides")
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(data) {
		return nil, fmt.Errorf("padding invalide : %d", pad)
	}
	return data[:len(data)-pad], nil
}

// xorEncrypt chiffre une string avec le XOR custom du SDK TCL (newEncrypt)
func xorEncrypt(str, key string) string {
	if str == "" {
		return ""
	}
	result := make([]byte, len(str)*2)
	for i := 0; i < len(str); i++ {
		k := key[i%len(key)]
		c := str[i]
		result[2*i] = (k & 0xf0) | ((c & 0xf) ^ (k & 0xf))
		result[2*i+1] = (k & 0xf0) | ((c >> 4) ^ (k & 0xf))
	}
	return string(result)
}

// pbkdf2Password génère le hash du mot de passe avec PBKDF2-SHA512
// comme le fait crypto_utils.pbkdf2() dans le SDK
func pbkdf2Password(password, salt string) string {
	derived := pbkdf2.Key([]byte(password), []byte(salt), 1024, 64, sha512.New)
	return fmt.Sprintf("%x", derived)
}

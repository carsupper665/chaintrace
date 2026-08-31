package controller

import (
	"crypto/sha256"
	"crypto/subtle"
	"math/big"
	"strings"
)

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

const maxBase58AddressLength = 64

// IsValidTRONAddress reports whether text is a well-formed TRON Base58Check
// address. It is exported because the Agent's tool arguments are model-written
// text that must pass the same check as an Owner's typed input.
func IsValidTRONAddress(address string) bool {
	if len(address) > maxBase58AddressLength {
		return false
	}
	number := new(big.Int)
	base := big.NewInt(58)
	for i := 0; i < len(address); i++ {
		index := strings.IndexByte(base58Alphabet, address[i])
		if index < 0 {
			return false
		}
		number.Mul(number, base)
		number.Add(number, big.NewInt(int64(index)))
	}

	decoded := number.Bytes()
	for leadingZeroes := 0; leadingZeroes < len(address) && address[leadingZeroes] == '1'; leadingZeroes++ {
		decoded = append([]byte{0}, decoded...)
	}
	if len(decoded) != 25 || decoded[0] != 0x41 {
		return false
	}
	firstHash := sha256.Sum256(decoded[:21])
	secondHash := sha256.Sum256(firstHash[:])
	return subtle.ConstantTimeCompare(decoded[21:], secondHash[:4]) == 1
}

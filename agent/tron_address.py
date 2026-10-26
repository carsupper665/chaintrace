"""TRON Base58Check 驗證 —— 與 Go controller.IsValidTRONAddress 同一套規則。"""

from __future__ import annotations

import hashlib

_BASE58_ALPHABET = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
_MAX_BASE58_ADDRESS_LENGTH = 64


def is_valid_tron_address(address: str) -> bool:
    if not address or len(address) > _MAX_BASE58_ADDRESS_LENGTH:
        return False
    number = 0
    for character in address:
        index = _BASE58_ALPHABET.find(character)
        if index < 0:
            return False
        number = number * 58 + index

    decoded = number.to_bytes((number.bit_length() + 7) // 8, "big") if number else b""
    leading_zeroes = 0
    for character in address:
        if character != "1":
            break
        leading_zeroes += 1
    decoded = (b"\x00" * leading_zeroes) + decoded
    if len(decoded) != 25 or decoded[0] != 0x41:
        return False
    first_hash = hashlib.sha256(decoded[:21]).digest()
    second_hash = hashlib.sha256(first_hash).digest()
    return decoded[21:] == second_hash[:4]

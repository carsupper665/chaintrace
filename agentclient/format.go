package agentclient

import (
	"math/big"
	"strings"

	"chaintrace/model/store"
)

// summarize is the one place a stored transfer becomes something the Agent
// reads, so every path presents amounts the same way.
func summarize(transfer store.TRC20Transfer) TransferSummary {
	return TransferSummary{
		TransactionHash: transfer.TransactionHash,
		From:            transfer.FromAddress,
		To:              transfer.ToAddress,
		AmountDisplay: formatExact(
			transfer.AmountSmallestUnit, transfer.Decimals, transfer.Asset),
		Timestamp: transfer.Timestamp,
	}
}

// formatExact renders a smallest-unit amount without touching floating point,
// so a large USDT figure reaches the Agent digit-for-digit.
func formatExact(smallestUnit string, decimals int, asset string) string {
	value, ok := new(big.Int).SetString(smallestUnit, 10)
	if !ok {
		return ""
	}
	text := value.String()
	negative := strings.HasPrefix(text, "-")
	text = strings.TrimPrefix(text, "-")
	whole, fraction := text, ""
	if decimals > 0 {
		if len(text) <= decimals {
			whole = "0"
			fraction = strings.Repeat("0", decimals-len(text)) + text
		} else {
			whole = text[:len(text)-decimals]
			fraction = text[len(text)-decimals:]
		}
		fraction = strings.TrimRight(fraction, "0")
	}
	builder := strings.Builder{}
	if negative {
		builder.WriteString("-")
	}
	builder.WriteString(groupThousands(whole))
	if fraction != "" {
		builder.WriteString("." + fraction)
	}
	if asset != "" {
		builder.WriteString(" " + asset)
	}
	return builder.String()
}

func groupThousands(digits string) string {
	if len(digits) <= 3 {
		return digits
	}
	lead := len(digits) % 3
	parts := make([]string, 0, len(digits)/3+1)
	if lead > 0 {
		parts = append(parts, digits[:lead])
	}
	for index := lead; index < len(digits); index += 3 {
		parts = append(parts, digits[index:index+3])
	}
	return strings.Join(parts, ",")
}

func truncate[T any](values []T, limit int) []T {
	if len(values) > limit {
		return values[:limit]
	}
	if values == nil {
		return []T{}
	}
	return values
}

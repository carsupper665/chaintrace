package auth

var (
	RTS *RevokedTokenRegistry
)

func InitAuth() error {
	var err error
	RTS = NewRevokedTokenRegistry()
	return err
}

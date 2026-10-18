package user

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"proxycontrol/internal/platform/id"
)

func GenerateCredential(protocol string) (Secret, error) {
	if err := ValidateProtocols([]string{protocol}); err != nil {
		return Secret{}, err
	}
	switch protocol {
	case "vless":
		data := make([]byte, 16)
		if _, err := rand.Read(data); err != nil {
			return Secret{}, err
		}
		data[6] = (data[6] & 15) | 64
		data[8] = (data[8] & 63) | 128
		encoded := hex.EncodeToString(data)
		return Secret{Value: encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:]}, nil
	case "wireguard":
		private, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			return Secret{}, err
		}
		return Secret{Value: base64.StdEncoding.EncodeToString(private.Bytes()), PublicKey: base64.StdEncoding.EncodeToString(private.PublicKey().Bytes())}, nil
	case "shadowsocks":
		data := make([]byte, 32)
		if _, err := rand.Read(data); err != nil {
			return Secret{}, err
		}
		return Secret{Value: base64.StdEncoding.EncodeToString(data)}, nil
	case "tuic":
		identifier, err := GenerateCredential("vless")
		if err != nil {
			return Secret{}, err
		}
		password, err := id.New()
		return Secret{Value: password, Username: identifier.Value}, err
	case "http", "socks5", "trojan", "hysteria2":
		value, err := id.New()
		return Secret{Value: value}, err
	}
	return Secret{}, errors.New("unsupported credential protocol")
}

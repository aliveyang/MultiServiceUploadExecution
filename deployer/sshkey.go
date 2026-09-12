package deployer

import (
	"encoding/pem"
	"errors"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// PrivateKeyMeta 解析私钥内容并返回算法类型与 SHA256 公钥指纹（格式同 ssh-keygen，
// 如 "SHA256:abcdef..."，与 ServerConfig.HostKeyFingerprint 校验格式一致）。
// 私钥内容本身不会被返回或记录；加密私钥需提供 passphrase 才能推导指纹（仅用于推导，不存储）。
func PrivateKeyMeta(pemBytes []byte, passphrase string) (algorithm, fingerprint string, err error) {
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		var missing *ssh.PassphraseMissingError
		if !errors.As(err, &missing) {
			return "", "", fmt.Errorf("invalid private key: %w", err)
		}
		algo := pemAlgorithm(pemBytes)
		if passphrase == "" {
			// 加密私钥且未提供 passphrase：算法可识别，但无法离线推导公钥指纹
			return algo, "", nil
		}
		s2, err2 := ssh.ParsePrivateKeyWithPassphrase(pemBytes, []byte(passphrase))
		if err2 != nil {
			return algo, "", fmt.Errorf("passphrase incorrect or key unreadable: %w", err2)
		}
		pk := s2.PublicKey()
		return pk.Type(), ssh.FingerprintSHA256(pk), nil
	}

	pk := signer.PublicKey()
	return pk.Type(), ssh.FingerprintSHA256(pk), nil
}

// pemAlgorithm 从 PEM 头推断私钥算法类别（用于加密私钥无法取得公钥时的降级展示）
func pemAlgorithm(pemBytes []byte) string {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return "unknown"
	}
	switch block.Type {
	case "RSA PRIVATE KEY":
		return "RSA"
	case "EC PRIVATE KEY":
		return "EC"
	case "DSA PRIVATE KEY":
		return "DSA"
	case "OPENSSH PRIVATE KEY":
		return "OpenSSH"
	case "PRIVATE KEY":
		return "PKCS#8"
	default:
		return block.Type
	}
}

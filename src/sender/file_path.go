package sender

import (
	"fmt"
	"path"
	"strings"

	"forward-stub/src/packet"
)

func pathBase(v string) string {
	b := path.Base(strings.TrimSpace(v))
	if b == "." || b == "/" {
		return ""
	}
	return b
}

func safeRelativePath(v string) (string, error) {
	v = strings.ReplaceAll(strings.TrimSpace(v), "\\", "/")
	for _, part := range strings.Split(v, "/") {
		if part == ".." {
			return "", fmt.Errorf("unsafe path %q", v)
		}
	}
	v = strings.TrimPrefix(v, "/")
	v = path.Clean("/" + v)
	v = strings.TrimPrefix(v, "/")
	if v == "." || v == "" {
		return "", fmt.Errorf("empty path")
	}
	if v == ".." || strings.HasPrefix(v, "../") || strings.Contains(v, "/../") {
		return "", fmt.Errorf("unsafe path %q", v)
	}
	return v, nil
}

func applyTargetFileName(rel, targetName string) string {
	targetName = pathBase(targetName)
	if targetName == "" {
		return rel
	}
	dir := path.Dir(rel)
	if dir == "." || dir == "/" || dir == "" {
		return targetName
	}
	return path.Join(dir, targetName)
}

func sftpFinalPath(remoteDir string, p *packet.Packet, transferID string) (string, error) {
	candidate := strings.TrimSpace(p.Meta.TargetFilePath)
	if candidate == "" {
		candidate = strings.TrimSpace(p.Meta.FilePath)
	}
	if candidate == "" {
		candidate = strings.TrimSpace(p.Meta.FileName)
	}
	if candidate == "" {
		candidate = transferID + ".bin"
	}
	rel, err := safeRelativePath(candidate)
	if err != nil {
		return "", err
	}
	if p.Meta.TargetFilePath == "" && strings.TrimSpace(p.Meta.TargetFileName) != "" {
		rel = applyTargetFileName(rel, p.Meta.TargetFileName)
	}
	return path.Join(remoteDir, rel), nil
}

package pipeline

import (
	"path"
	"regexp"
	"strings"

	"forward-stub/src/packet"
)

func SetTargetFilePath(value string) StageFunc {
	return mapStage(func(p *packet.Packet) bool {
		p.Meta.TargetFilePath = value
		return true
	})
}

func RewriteTargetPathStripPrefix(prefix string) StageFunc {
	prefix = strings.TrimSpace(prefix)
	return mapStage(func(p *packet.Packet) bool {
		cur := targetPathOrSource(p)
		p.Meta.TargetFilePath = strings.TrimPrefix(cur, prefix)
		p.Meta.TargetFilePath = strings.TrimPrefix(p.Meta.TargetFilePath, "/")
		return true
	})
}

func RewriteTargetPathAddPrefix(prefix string) StageFunc {
	prefix = cleanPathPrefix(prefix)
	return mapStage(func(p *packet.Packet) bool {
		cur := strings.TrimPrefix(targetPathOrSource(p), "/")
		if prefix == "" {
			p.Meta.TargetFilePath = cur
			return true
		}
		p.Meta.TargetFilePath = path.Join(prefix, cur)
		return true
	})
}

func RewriteTargetFilenameReplace(old, new string) StageFunc {
	return mapStage(func(p *packet.Packet) bool {
		cur := targetFilenameOrSource(p)
		p.Meta.TargetFileName = strings.ReplaceAll(cur, old, new)
		p.Meta.TargetFilePath = replacePathBase(targetPathOrSource(p), p.Meta.TargetFileName)
		return true
	})
}

func RewriteTargetPathRegex(re *regexp.Regexp, replacement string) StageFunc {
	return mapStage(func(p *packet.Packet) bool {
		p.Meta.TargetFilePath = re.ReplaceAllString(targetPathOrSource(p), replacement)
		return true
	})
}

func RewriteTargetFilenameRegex(re *regexp.Regexp, replacement string) StageFunc {
	return mapStage(func(p *packet.Packet) bool {
		p.Meta.TargetFileName = re.ReplaceAllString(targetFilenameOrSource(p), replacement)
		p.Meta.TargetFilePath = replacePathBase(targetPathOrSource(p), p.Meta.TargetFileName)
		return true
	})
}

func targetPathOrSource(p *packet.Packet) string {
	if p == nil {
		return ""
	}
	if strings.TrimSpace(p.Meta.TargetFilePath) != "" {
		return p.Meta.TargetFilePath
	}
	return p.Meta.FilePath
}

func targetFilenameOrSource(p *packet.Packet) string {
	if p == nil {
		return ""
	}
	if strings.TrimSpace(p.Meta.TargetFileName) != "" {
		return p.Meta.TargetFileName
	}
	if strings.TrimSpace(p.Meta.FileName) != "" {
		return p.Meta.FileName
	}
	return path.Base(targetPathOrSource(p))
}

func replacePathBase(filePath, fileName string) string {
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		return filePath
	}
	dir := path.Dir(filePath)
	if dir == "." || dir == "/" || dir == "" {
		return fileName
	}
	return path.Join(dir, fileName)
}

func cleanPathPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return ""
	}
	return strings.Trim(path.Clean("/"+prefix), "/")
}

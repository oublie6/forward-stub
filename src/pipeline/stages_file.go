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

// RewriteTargetPathStripPrefix 从“当前目标路径，或来源路径”中去掉前缀。
// 前缀不匹配时 strings.TrimPrefix 保持原值；随后统一去掉开头的 "/"，
// 让 SFTP/OSS sender 后续只处理相对路径，避免 stage 之间重复关心路径安全。
func RewriteTargetPathStripPrefix(prefix string) StageFunc {
	prefix = strings.TrimSpace(prefix)
	return mapStage(func(p *packet.Packet) bool {
		cur := targetPathOrSource(p)
		p.Meta.TargetFilePath = strings.TrimPrefix(cur, prefix)
		p.Meta.TargetFilePath = strings.TrimPrefix(p.Meta.TargetFilePath, "/")
		return true
	})
}

// RewriteTargetPathAddPrefix 为目标路径增加规范化后的相对前缀。
// 当前路径为空时不会凭空生成文件名；前缀会通过 path.Clean 规整，避免多余斜杠影响 sender key/path。
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

// RewriteTargetFilenameReplace 改写目标文件名，并同步替换目标路径 basename。
// 这样下游 sender 只需要读取 TargetFilePath 就能拿到与 TargetFileName 一致的最终路径。
func RewriteTargetFilenameReplace(old, new string) StageFunc {
	return mapStage(func(p *packet.Packet) bool {
		cur := targetFilenameOrSource(p)
		p.Meta.TargetFileName = strings.ReplaceAll(cur, old, new)
		p.Meta.TargetFilePath = replacePathBase(targetPathOrSource(p), p.Meta.TargetFileName)
		return true
	})
}

// RewriteTargetPathRegex 用正则改写目标路径；不匹配时 regexp.ReplaceAllString 会保持原路径。
func RewriteTargetPathRegex(re *regexp.Regexp, replacement string) StageFunc {
	return mapStage(func(p *packet.Packet) bool {
		p.Meta.TargetFilePath = re.ReplaceAllString(targetPathOrSource(p), replacement)
		return true
	})
}

// RewriteTargetFilenameRegex 用正则改写目标文件名，并把目标路径 basename 同步为改写后的文件名。
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
	base := path.Base(strings.TrimSpace(targetPathOrSource(p)))
	if base == "." || base == "/" {
		return ""
	}
	return base
}

func replacePathBase(filePath, fileName string) string {
	fileName = strings.TrimSpace(fileName)
	if fileName == "" {
		if path.Base(strings.TrimSpace(filePath)) == "." {
			return ""
		}
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

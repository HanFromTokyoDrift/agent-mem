package main

import (
	"regexp"
	"strings"
)

// 分割优先级正则
var (
	splitDoubleNewline = regexp.MustCompile(`\n\n`)
	splitHeader        = regexp.MustCompile(`\n#{1,6}\s`)
	splitList          = regexp.MustCompile(`\n[\*\-\+]\s|\n\d+\.\s`)
	splitNewline       = regexp.MustCompile(`\n`)
	splitSentence      = regexp.MustCompile(`[。．\.!！\?？]\s*`)
)

func chunkContent(content string, cfg ChunkingConfig) []string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return []string{}
	}

	chunkSize := cfg.ChunkSize
	overlap := cfg.Overlap
	charsPerToken := cfg.ApproxCharsPerToken
	if chunkSize <= 0 {
		chunkSize = 500
	}
	if overlap < 0 {
		overlap = 0
	}
	if charsPerToken <= 0 {
		charsPerToken = 4
	}

	targetChars := chunkSize * charsPerToken // 目标长度 (e.g. 2000)
	maxChars := int(float64(targetChars) * 1.25) // 允许最大溢出 25%

	runes := []rune(trimmed)
	totalLen := len(runes)
	if totalLen <= maxChars {
		return []string{string(runes)}
	}

	var chunks []string
	start := 0

	for start < totalLen {
		// 如果剩余内容少于最大限制，直接作为最后一块
		if totalLen-start <= maxChars {
			chunks = append(chunks, strings.TrimSpace(string(runes[start:])))
			break
		}

		// 寻找最佳分割点
		// 我们希望分割点在 [targetChars, maxChars] 之间，如果找不到，就往前找 [minChars, targetChars]
		// 这里的逻辑简化为：在 [start+targetChars/2, start+maxChars] 范围内寻找最佳分割点
		
		searchStart := start + targetChars/2
		searchEnd := start + maxChars
		if searchEnd > totalLen {
			searchEnd = totalLen
		}
		
		// 截取候选区段的字符串用于正则匹配
		candidateSection := string(runes[searchStart:searchEnd])
		
		splitOffset := -1
		
		// 1. 尝试双换行 (段落)
		if loc := splitDoubleNewline.FindStringIndex(candidateSection); loc != nil {
			// loc[0] 是匹配开始的字节索引，我们需要转换为 rune 索引... 
			// 这种混用 byte index 和 rune index 很危险，改用更稳健的倒序扫描法
			splitOffset = findBestSplitPoint(runes, searchStart, searchEnd)
		} else {
             // 如果没有正则匹配库（Go regexp 是 byte based），我们手写倒序扫描更安全
             splitOffset = findBestSplitPoint(runes, searchStart, searchEnd)
        }
        
        // 如果找到了分割点
		end := splitOffset
		if splitOffset == -1 {
			// 没找到自然分割点，强制在 maxChars 处切断 (避免死循环)
			end = start + targetChars 
            if end > totalLen {
                end = totalLen
            }
		}

		chunk := strings.TrimSpace(string(runes[start:end]))
		if chunk != "" {
			chunks = append(chunks, chunk)
		}

		// 计算下一个 start (考虑 overlap)
		// 注意：这里的 overlap 也是字符数
		// 如果是自然分割，overlap 意义不大，甚至可能导致重复段落。
        // 但为了保证上下文连续性，我们通常回退一点，或者如果是硬切分则必须回退。
        // 改进策略：如果是强制切分，回退 overlap；如果是自然段落切分，不回退（除非这会导致丢失）
        
        if splitOffset == -1 {
             // 强制切分，应用 overlap
             nextStart := end - (overlap * charsPerToken)
             if nextStart <= start {
                 nextStart = end // 避免回退过多导致死循环
             }
             start = nextStart
        } else {
             // 自然切分，通常不需要 overlap，或者只回退少量上下文
             // 这里简单处理：直接从分割点继续，因为段落分割本身就暗示了上下文边界
             start = end
        }
	}
	return chunks
}

// 在指定范围内 [min, max) 倒序寻找最佳分割点
// 返回的是全局 rune index
func findBestSplitPoint(runes []rune, minIdx, maxIdx int) int {
    if minIdx >= maxIdx || maxIdx > len(runes) {
        return -1
    }
    
    // 优先级 1: 双换行 \n\n
    for i := maxIdx - 1; i >= minIdx; i-- {
        if i > 0 && runes[i] == '\n' && runes[i-1] == '\n' {
            return i + 1 // 切在换行符之后
        }
    }
    
    // 优先级 2: 标题 (Markdown Header) \n# 
    for i := maxIdx - 1; i >= minIdx; i-- {
        if i > 1 && runes[i] == ' ' && runes[i-1] == '#' && runes[i-2] == '\n' {
             return i - 2 // 切在标题之前
        }
    }
    
    // 优先级 3: 列表项 \n- 
    for i := maxIdx - 1; i >= minIdx; i-- {
         if i > 1 && runes[i] == ' ' && (runes[i-1] == '-' || runes[i-1] == '*') && runes[i-2] == '\n' {
             return i - 2
         }
    }
    
    // 优先级 4: 句号 (中文或英文)
    for i := maxIdx - 1; i >= minIdx; i-- {
         if runes[i] == '。' || (runes[i] == '.' && i+1 < len(runes) && runes[i+1] == ' ') {
             return i + 1
         }
    }
    
    // 优先级 5: 单换行
    for i := maxIdx - 1; i >= minIdx; i-- {
        if runes[i] == '\n' {
            return i + 1
        }
    }
    
    return -1 // 没找到
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

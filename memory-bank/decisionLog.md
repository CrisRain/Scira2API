# 项目决策日志

## 2025/5/22 - Scira Chat 接口流式转发实现方案

**背景:** 需要优化 scria chat 接口，使其支持将从 scria 服务收到的分块数据实时流式转发给客户端。

**决策:**
1.  **核心处理函数:** 确定修改 [`service/chatHandler.go`](service/chatHandler.go:0) 中的 `handleStreamResponse` (在 [`service/chatHandler.go:162`](service/chatHandler.go:162) ) 函数为主要改造点。
2.  **流式技术选型:**
    *   初步尝试：使用 `bufio.NewScanner(resp.Body)` (在 [`service/chatHandler.go:176`](service/chatHandler.go:176) ) 和 `bufio.NewReader(resp.Body).ReadString('\n')` (在 [`bufio.Reader.ReadString()`](bufio:510) )。
    *   最终方案：由于 `ReadString` (在 [`bufio.Reader.ReadString()`](bufio:510) ) 仍存在缓冲问题，改为直接使用 `resp.Body.Read(buffer)` (在 [`io.Reader.Read()`](io:0) ) 读取字节流，并手动进行行缓冲和解析，以实现更实时的转发。同时，客户端响应采用 Gin 框架的 `c.Stream()` (在 [`github.com/gin-gonic/gin/context.go`](github.com/gin-gonic/gin/context.go) ) 方法。
3.  **Scria 特定前缀处理:**
    *   根据用户反馈和日志分析，识别并处理了 scria 服务特有的流数据前缀，包括 `g:`, `0:`, `e:`, `d:`, `f:`, `j:` (在 [`service/chatHandler.go:188`](service/chatHandler.go:188) )。
    *   对于 `d:` (在 [`service/chatHandler.go:221`](service/chatHandler.go:221) ) 前缀，提取 `finishReason` (在 [`service/chatHandler.go:225`](service/chatHandler.go:225) )。
    *   对于 `f:` (在 [`service/chatHandler.go:234`](service/chatHandler.go:234) ) 和 `j:` (在 [`service/chatHandler.go:234`](service/chatHandler.go:234) ) 等内部信令，进行日志记录但跳过向客户端转发。

**理由:**
*   直接读取字节流并手动解析能最大限度地减少延迟，确保数据块一旦可用即被转发。
*   `c.Stream()` (在 [`github.com/gin-gonic/gin/context.go`](github.com/gin-gonic/gin/context.go) ) 是 Gin 框架推荐的流式响应方式。
*   对特定前缀的细致处理保证了与 scria 服务协议的兼容性和数据的正确解析。

**记录来源:** 子任务执行过程，记录于 [`memory-bank/activeContext.md`](memory-bank/activeContext.md) (版本：2025/5/22 子任务完成时)。
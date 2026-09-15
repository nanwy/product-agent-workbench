# Pending 属于客户端已拥有内容

用户已经能准确说明：pending delta 虽然尚未显示，却已经被客户端接收，因此必须包含在 effective content 中；否则相同文本会被下一次快照再次截取，最终造成重复追加。这一理解解锁了对 `bufferableSnapshot` 前缀比较和帧缓冲正确性的代码级讲解。

## Evidence

用户独立纠正并表述：“Pending 也算即将显示在页面里面的已有内容，不然同一段文字会被截取两次。”

package render

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"net/http"
	"time"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"github.com/guohuiyuan/qzonewall-go/internal/model"
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
)

//go:embed font.ttf
var fontData []byte

type Renderer struct {
	font *truetype.Font
}

func NewRenderer(fontPath string, fontSize float64) *Renderer {
	f, err := truetype.Parse(fontData)
	if err != nil {
		log.Printf("[Renderer] 解析内置字体失败: %v", err)
		return &Renderer{font: nil}
	}
	return &Renderer{font: f}
}

func (r *Renderer) Available() bool {
	return r.font != nil
}

func (r *Renderer) getFace(size float64) font.Face {
	if r.font == nil {
		return nil
	}
	return truetype.NewFace(r.font, &truetype.Options{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}

// RenderPost 渲染图文合一
func (r *Renderer) RenderPost(post *model.Post) ([]byte, error) {
	if !r.Available() {
		return nil, fmt.Errorf("渲染器未初始化")
	}

	// ── 1. 样式常量 ──
	const (
		CanvasWidth = 800.0
		Padding     = 40.0

		SizeText = 32.0
		SizeName = 26.0
		SizeMeta = 22.0

		AvatarSize  = 80.0
		AvatarRight = 20.0 // 头像和内容的间距

		BubblePadH = 30.0
		BubblePadV = 25.0
		LineHeight = 1.4

		// 图片九宫格配置
		ImgGap  = 10.0 // 图片间距
		ImgSize = 220.0 // 单张小图大小 (3列)
	)

	// ── 2. 计算布局高度 ──

	// A. 计算文字气泡
	// 内容最大宽度 = 画布宽 - 两边边距 - 头像 - 间距
	contentMaxW := CanvasWidth - (Padding * 2) - AvatarSize - AvatarRight
	
	// 创建临时 Context 测量文字
	dc := gg.NewContext(1, 1)
	textFace := r.getFace(SizeText) // 获取字体对象
	dc.SetFontFace(textFace)

	var lines []string
	if post.Text != "" {
		lines = dc.WordWrap(post.Text, contentMaxW-(BubblePadH*2))
	}

	fontH := dc.FontHeight()
	// 文字区域高度
	textBlockH := 0.0
	if len(lines) > 0 {
		textBlockH = float64(len(lines)) * fontH * LineHeight
	}
	
	// 气泡高度 = 文字高 + 内边距
	bubbleH := 0.0
	if len(lines) > 0 {
		bubbleH = textBlockH + (BubblePadV * 2)
	}

	// B. 计算图片区域
	imgAreaH := 0.0
	imgCount := len(post.Images)
	var imgCols, imgRows int

	if imgCount > 0 {
		// 只有1张图：显示大图
		if imgCount == 1 {
			imgCols = 1
			imgRows = 1
			imgAreaH = 400.0 // 单张图限制高度
		} else {
			// 多张图：九宫格
			imgCols = 3
			if imgCount == 2 || imgCount == 4 {
				imgCols = 2 // 2张或4张时排两列更好看
			}
			imgRows = int(math.Ceil(float64(imgCount) / float64(imgCols)))
			// 总高度 = 行数*图高 + (行数-1)*间距
			imgAreaH = float64(imgRows)*ImgSize + float64(imgRows-1)*ImgGap
		}
	}

	// C. 计算总高度
	// 结构：顶部Padding + 昵称 + (文字气泡) + 间距 + (图片区域) + 底部信息 + 底部Padding
	
	currentY := Padding
	
	// 昵称高度
	currentY += SizeName + 10 
	
	contentStartY := currentY // 内容起始点

	// 累加气泡高度
	if bubbleH > 0 {
		currentY += bubbleH
	}
	
	// 累加图片高度
	if imgAreaH > 0 {
		if bubbleH > 0 {
			currentY += 15.0 // 文字和图片之间的间距
		}
		currentY += imgAreaH
	}

	// 底部 ID 信息
	currentY += 40.0 
	currentY += Padding

	totalH := int(currentY)
	// 保证最小高度不至于截断头像
	if totalH < int(Padding+AvatarSize+Padding) {
		totalH = int(Padding + AvatarSize + Padding)
	}

	// ── 3. 开始绘制 ──
	dc = gg.NewContext(int(CanvasWidth), totalH)
	
	// 背景色
	dc.SetHexColor("#F5F5F5") 
	dc.Clear()

	// 坐标原点
	startX := Padding
	startY := Padding

	// 3.1 绘制头像
	avatarImg := downloadAndResize(post.QQAvatarURL(), int(AvatarSize), int(AvatarSize))
	dc.Push()
	dc.DrawCircle(startX+AvatarSize/2, startY+AvatarSize/2, AvatarSize/2)
	dc.Clip()
	if avatarImg != nil {
		dc.DrawImageAnchored(avatarImg, int(startX+AvatarSize/2), int(startY+AvatarSize/2), 0.5, 0.5)
	} else {
		dc.SetHexColor("#DCDCDC")
		dc.DrawRectangle(startX, startY, AvatarSize, AvatarSize)
		dc.Fill()
	}
	dc.Pop()

	// 内容左边距 (头像右侧)
	contentX := startX + AvatarSize + AvatarRight
	
	// 3.2 绘制昵称
	dc.SetFontFace(r.getFace(SizeName))
	dc.SetHexColor("#888888")
	// 昵称在头像右侧，顶部对齐
	dc.DrawString(post.ShowName(), contentX, startY+SizeName-5)

	currContentY := contentStartY

	// 3.3 绘制文字气泡 (如果有文字)
	if bubbleH > 0 {
		dc.SetColor(color.White)
		dc.DrawRoundedRectangle(contentX, currContentY, contentMaxW, bubbleH, 12)
		dc.Fill()

		// 小三角
		dc.MoveTo(contentX, currContentY+20)
		dc.LineTo(contentX-8, currContentY+28)
		dc.LineTo(contentX, currContentY+36)
		dc.ClosePath()
		dc.Fill()

		// 绘制文字内容
		dc.SetFontFace(textFace) // 使用之前获取的 face
		dc.SetHexColor("#000000")
		
		// 修正文字垂直对齐
		metrics := textFace.Metrics() // ★★★ 修复点：直接使用 textFace 获取 Metrics
		ascent := float64(metrics.Ascent.Ceil())
		
		textY := currContentY + BubblePadV + ascent
		for i, line := range lines {
			dc.DrawString(line, contentX+BubblePadH, textY+float64(i)*fontH*LineHeight)
		}
		
		currContentY += bubbleH + 15.0
	}

	// 3.4 绘制图片 (如果有)
	if imgCount > 0 {
		// 下载所有图片 (为了简单这里串行下载，量大建议并发)
		// 如果只有一张图，尝试按比例缩放，限制最大宽/高
		if imgCount == 1 {
			// 下载原始大图
			rawImg := downloadImage(post.Images[0])
			if rawImg != nil {
				// 计算缩放比例
				bounds := rawImg.Bounds()
				w, h := float64(bounds.Dx()), float64(bounds.Dy())
				
				// 限制最大宽高
				maxW := 350.0
				maxH := 400.0
				scale := math.Min(maxW/w, maxH/h)
				// 如果图片本身就很小，不放大
				if scale > 1.0 { scale = 1.0 }
				
				targetW := int(w * scale)
				targetH := int(h * scale)
				
				// 缩放
				finalImg := resizeImage(rawImg, targetW, targetH)
				
				// 绘制
				dc.DrawImage(finalImg, int(contentX), int(currContentY))
			}
		} else {
			// 多图九宫格
			for i, imgUrl := range post.Images {
				if i >= 9 { break } // 最多显示9张
				
				col := i % imgCols
				row := i / imgCols
				
				ix := contentX + float64(col)*(ImgSize+ImgGap)
				iy := currContentY + float64(row)*(ImgSize+ImgGap)
				
				// 下载并裁剪为正方形
				img := downloadAndResize(imgUrl, int(ImgSize), int(ImgSize))
				if img != nil {
					// 画圆角图片
					dc.Push()
					dc.DrawRoundedRectangle(ix, iy, ImgSize, ImgSize, 8)
					dc.Clip()
					dc.DrawImage(img, int(ix), int(iy))
					dc.Pop()
				}
			}
		}
	}

	// 3.5 底部水印
	dc.SetFontFace(r.getFace(SizeMeta))
	dc.SetHexColor("#CCCCCC")
	// 放在右下角
	dc.DrawStringAnchored(fmt.Sprintf("Post #%d  %s", post.ID, time.Now().Format("15:04")), CanvasWidth-Padding, float64(totalH)-Padding/2, 1.0, 1.0)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dc.Image()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// downloadImage 仅下载不缩放
func downloadImage(url string) image.Image {
	if url == "" { return nil }
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil { return nil }
	defer resp.Body.Close()
	img, _, err := image.Decode(resp.Body)
	if err != nil { return nil }
	return img
}

// resizeImage 指定宽高缩放
func resizeImage(src image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	return dst
}

// downloadAndResize 下载并强制裁剪/缩放为指定大小 (用于头像和九宫格)
func downloadAndResize(url string, w, h int) image.Image {
	src := downloadImage(url)
	if src == nil { return nil }
	
	// 如果是正方形裁剪模式（宽高一致）
	if w == h {
		// 先裁剪成正方形，取中间部分
		bounds := src.Bounds()
		bw, bh := bounds.Dx(), bounds.Dy()
		minSide := bw
		if bh < bw { minSide = bh }
		
		// 计算裁剪区域中心
		cx, cy := bw/2, bh/2
		half := minSide / 2
		cropRect := image.Rect(cx-half, cy-half, cx+half, cy+half)
		
		// 这里的 subImage 实现比较简单，配合 draw 库使用
		// 实际上我们需要创建一个新的 Img 对象
		// 为了简单，直接缩放整图，可能会变形，更好的做法是 Crop + Scale
		// 这里使用 CatmullRom 缩放，如果原图比例差异大可能会变形，
		// 严谨的做法是先 Crop Center 再 Resize。
		
		// 简易 Crop & Scale:
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		// 使用 draw.ApproxBiLinear 或者 CatmullRom
		// 这里为了不引入太复杂的裁剪逻辑，直接缩放 (可能轻微变形)
		// 生产环境建议先裁剪 src
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, cropRect, draw.Over, nil)
		return dst
	}

	return resizeImage(src, w, h)
}
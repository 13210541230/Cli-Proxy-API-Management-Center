from pathlib import Path
import shutil
from PIL import Image, ImageOps
from pptx import Presentation
from pptx.dml.color import RGBColor
from pptx.enum.shapes import MSO_SHAPE, MSO_CONNECTOR
from pptx.enum.text import PP_ALIGN, MSO_ANCHOR
from pptx.util import Inches, Pt

ROOT = Path(__file__).resolve().parent
TEMPLATE = Path(r"D:/Downloads/2026年金码奖申报陈述PPT模板-XX部门-姓名.pptx")
OUTPUT = ROOT / "jinma-award-statement-expanded.pptx"
CURRENT = ROOT / "jinma-award-statement.pptx"
NAMED = ROOT / "2026年金码奖申报陈述PPT-研发中心四部-吴强辉.pptx"
PHOTO = Path(r"D:/Pictures/吴强辉.jpg")
PHOTO_ASSET = ROOT / "media" / "profile-wuqh.png"

RED = RGBColor(0xED, 0x00, 0x16)
BLUE = RGBColor(0x06, 0x67, 0xB1)
INK = RGBColor(0x22, 0x22, 0x22)
MUTED = RGBColor(0x66, 0x66, 0x66)
PALE = RGBColor(0xF5, 0xF7, 0xF9)
WHITE = RGBColor(0xFF, 0xFF, 0xFF)
DARK = RGBColor(0x18, 0x23, 0x2D)
LIGHT_RED = RGBColor(0xFF, 0xF0, 0xF1)
LIGHT_BLUE = RGBColor(0xEE, 0xF6, 0xFC)

# Shared content grid: repeated controls use stable left/right boundaries.
GRID_LEFT = 0.82
GRID_4_GAP = 0.22
GRID_4_W = 2.76
GRID_4_XS = [0.82, 3.80, 6.78, 9.76]
GRID_3_W = 3.59
GRID_3_XS = [0.82, 4.87, 8.92]
GRID_2_W = 5.635
GRID_2_XS = [0.82, 6.875]
SINGLE_IMAGE_X = 3.65
SINGLE_IMAGE_W = 6.03


def set_run(run, size=18, bold=False, color=INK, font="Microsoft YaHei", italic=False):
    run.font.name = font
    run.font.size = Pt(size)
    run.font.bold = bold
    run.font.italic = italic
    run.font.color.rgb = color


def clear_frame(shape):
    tf = shape.text_frame
    tf.clear()
    tf.word_wrap = True
    tf.margin_left = Inches(0.08)
    tf.margin_right = Inches(0.08)
    tf.margin_top = Inches(0.04)
    tf.margin_bottom = Inches(0.04)
    return tf


def clear_slide(slide):
    for shape in list(slide.shapes):
        slide.shapes._spTree.remove(shape._element)


def set_plain(shape, text, size=18, bold=False, color=INK, align=None, font="Microsoft YaHei"):
    tf = clear_frame(shape)
    p = tf.paragraphs[0]
    p.alignment = align if align is not None else PP_ALIGN.LEFT
    r = p.add_run(); r.text = text
    set_run(r, size=size, bold=bold, color=color, font=font)
    return tf


def replace_all_runs(shape, replacements):
    if not hasattr(shape, "text_frame"):
        return
    for p in shape.text_frame.paragraphs:
        for r in p.runs:
            for old, new in replacements.items():
                if old in r.text:
                    r.text = r.text.replace(old, new)


def add_text(slide, x, y, w, h, text, size=16, bold=False, color=INK,
             align=PP_ALIGN.LEFT, font="Microsoft YaHei", italic=False):
    shape = slide.shapes.add_textbox(Inches(x), Inches(y), Inches(w), Inches(h))
    tf = clear_frame(shape)
    tf.vertical_anchor = MSO_ANCHOR.TOP
    p = tf.paragraphs[0]
    p.alignment = align
    r = p.add_run(); r.text = text
    set_run(r, size=size, bold=bold, color=color, font=font, italic=italic)
    return shape


def add_page_title(slide, title, subtitle=None):
    add_text(slide, 0.33, 0.36, 12.2, 0.52, title, size=24, bold=True, color=MUTED)
    if subtitle:
        add_text(slide, 0.72, 0.94, 11.9, 0.34, subtitle, size=11.5, color=MUTED)


def add_box(slide, x, y, w, h, text, fill, line, size=14, bold=False,
            color=INK, align=PP_ALIGN.CENTER, radius=True):
    kind = MSO_SHAPE.ROUNDED_RECTANGLE if radius else MSO_SHAPE.RECTANGLE
    sh = slide.shapes.add_shape(kind, Inches(x), Inches(y), Inches(w), Inches(h))
    sh.fill.solid(); sh.fill.fore_color.rgb = fill
    sh.line.color.rgb = line; sh.line.width = Pt(1.1)
    tf = clear_frame(sh); tf.vertical_anchor = MSO_ANCHOR.MIDDLE
    p = tf.paragraphs[0]; p.alignment = align
    r = p.add_run(); r.text = text
    set_run(r, size=size, bold=bold, color=color)
    return sh


def add_arrow(slide, x1, y1, x2, y2, color=RED, width=1.8):
    line = slide.shapes.add_connector(MSO_CONNECTOR.STRAIGHT, Inches(x1), Inches(y1), Inches(x2), Inches(y2))
    line.line.color.rgb = color; line.line.width = Pt(width); line.line.end_arrowhead = True
    return line


def add_line(slide, x1, y1, x2, y2, color=RED, width=1.8):
    line = slide.shapes.add_connector(MSO_CONNECTOR.STRAIGHT, Inches(x1), Inches(y1), Inches(x2), Inches(y2))
    line.line.color.rgb = color; line.line.width = Pt(width)
    return line


def add_card(slide, x, y, w, h, title, body, accent=RED, fill=PALE,
             title_size=14, body_size=12.2):
    sh = slide.shapes.add_shape(MSO_SHAPE.RECTANGLE, Inches(x), Inches(y), Inches(w), Inches(h))
    sh.fill.solid(); sh.fill.fore_color.rgb = fill
    sh.line.color.rgb = accent; sh.line.width = Pt(1.0)
    tf = clear_frame(sh)
    tf.margin_left = Inches(0.16); tf.margin_right = Inches(0.14)
    tf.margin_top = Inches(0.12); tf.margin_bottom = Inches(0.08)
    p = tf.paragraphs[0]; p.space_after = Pt(6)
    r = p.add_run(); r.text = title; set_run(r, size=title_size, bold=True, color=accent)
    p2 = tf.add_paragraph(); p2.space_after = Pt(0)
    r2 = p2.add_run(); r2.text = body; set_run(r2, size=body_size, color=INK)
    return sh


def add_flow(slide, labels, x=0.8, y=2.05, box_w=2.2, box_h=0.72, gap=0.42):
    # All four-step flows share the same four-column grid.
    x = GRID_LEFT
    box_w = GRID_4_W
    gap = GRID_4_GAP
    for i, label in enumerate(labels):
        bx = x + i * (box_w + gap)
        fill, line, color = (RED, RED, WHITE) if i == 1 else (PALE, BLUE, INK)
        add_box(slide, bx, y, box_w, box_h, label, fill, line, size=13, bold=True, color=color)
        if i < len(labels)-1:
            add_arrow(slide, bx+box_w, y+box_h/2, bx+box_w+gap, y+box_h/2, BLUE)


def add_code_panel(slide, x, y, w, h, heading, code, accent=RED, code_size=9.0):
    panel = slide.shapes.add_shape(MSO_SHAPE.RECTANGLE, Inches(x), Inches(y), Inches(w), Inches(h))
    panel.fill.solid(); panel.fill.fore_color.rgb = DARK; panel.line.color.rgb = DARK
    add_text(slide, x+0.16, y+0.10, w-0.3, 0.28, heading, size=11, bold=True, color=WHITE, font="Arial")
    tb = slide.shapes.add_textbox(Inches(x+0.16), Inches(y+0.43), Inches(w-0.30), Inches(h-0.52))
    tf = clear_frame(tb); tf.margin_left = 0; tf.margin_right = 0; tf.margin_top = 0; tf.margin_bottom = 0
    for i, line in enumerate(code.splitlines()):
        p = tf.paragraphs[0] if i == 0 else tf.add_paragraph(); p.space_after = Pt(0)
        r = p.add_run(); r.text = line; set_run(r, size=code_size, color=WHITE, font="Consolas")
    return panel


def add_picture(slide, path, x, y, w, h, line=BLUE):
    pic = slide.shapes.add_picture(str(path), Inches(x), Inches(y), width=Inches(w), height=Inches(h))
    frame = slide.shapes.add_shape(MSO_SHAPE.RECTANGLE, Inches(x), Inches(y), Inches(w), Inches(h))
    frame.fill.background(); frame.line.color.rgb = line; frame.line.width = Pt(1.0)
    return pic


def new_inner_slide(prs):
    slide = prs.slides.add_slide(prs.slide_layouts[2])
    clear_slide(slide)
    return slide


def prepare_profile_photo():
    if not PHOTO.exists():
        raise FileNotFoundError(PHOTO)
    PHOTO_ASSET.parent.mkdir(parents=True, exist_ok=True)
    target = (770, 930)
    image = Image.open(PHOTO).convert("RGB")
    image = ImageOps.fit(image, target, method=Image.Resampling.LANCZOS, centering=(0.5, 0.42))
    image.save(PHOTO_ASSET)
    return PHOTO_ASSET


def populate_profile(slide):
    clear_slide(slide)
    add_page_title(slide, "自我介绍")
    rows = [
        ("姓名", "吴强辉"),
        ("所在部门", "研发中心四部"),
        ("当前职级", "P2｜C++开发工程师"),
        ("所属模块", "CLIProxyAPI"),
        ("代码仓库", "https://github.com/13210541230/CLIProxyAPI"),
        ("交付状态", "已推广使用"),
    ]
    start_y = 1.48
    for i, (label, value) in enumerate(rows):
        y = start_y + i * 0.68
        marker = slide.shapes.add_shape(MSO_SHAPE.RIGHT_ARROW, Inches(1.15), Inches(y + 0.10), Inches(0.18), Inches(0.16))
        marker.fill.solid(); marker.fill.fore_color.rgb = INK
        marker.line.color.rgb = INK
        add_text(slide, 1.48, y, 1.42, 0.40, label + "：", size=18, bold=True, color=INK)
        value_size = 11.5 if label == "代码仓库" else 18
        add_text(slide, 2.86, y, 4.35, 0.40, value, size=value_size, color=INK)
    photo_asset = prepare_profile_photo()
    photo_x, photo_y, photo_w, photo_h = 7.92, 1.30, 3.78, 4.60
    slide.shapes.add_picture(str(photo_asset), Inches(photo_x), Inches(photo_y), width=Inches(photo_w), height=Inches(photo_h))
    frame = slide.shapes.add_shape(MSO_SHAPE.RECTANGLE, Inches(photo_x), Inches(photo_y), Inches(photo_w), Inches(photo_h))
    frame.fill.background()
    frame.line.color.rgb = INK; frame.line.width = Pt(1.0)


def populate_highlights(slide):
    clear_slide(slide)
    add_page_title(slide, "申报亮点")
    # Use concrete, user-facing capabilities instead of a generic delivery claim.
    highlights = [
        ("01｜API Key 统一管理", "支持批量导入、CSV 预览、人员与部门绑定、历史查询和脱敏展示，明确凭证归属并保留可追溯记录。", RED, LIGHT_RED),
        ("02｜用量分析与统计", "采集请求、Token 与费用数据，按模型、部门、Key 等维度提供趋势、排行和明细查询，为资源配置与使用评估提供依据。", BLUE, LIGHT_BLUE),
        ("03｜认证登录与额度查看", "支持账号认证登录，集中查看认证状态、可用额度与使用情况，减少重复配置，便于及时掌握 AI 使用状态。", RED, LIGHT_RED),
        ("04｜风险请求标记与人工审计", "对命中 cyber_policy 的风险请求进行标记，支持人工查看脱敏后的用户请求内容并开展复核，降低公司账号资产损失风险。", BLUE, LIGHT_BLUE),
    ]
    positions = [(GRID_2_XS[0], 1.62), (GRID_2_XS[1], 1.62),
                 (GRID_2_XS[0], 4.00), (GRID_2_XS[1], 4.00)]
    for (title, body, accent, fill), (x, y) in zip(highlights, positions):
        add_card(slide, x, y, GRID_2_W, 1.78, title, body, accent, fill,
                 title_size=14, body_size=11.6)


def populate_background_phase(slide):
    clear_slide(slide)
    add_page_title(slide, "需求背景（一）｜企业进入规模化 AI 使用阶段",
                   "员工、模型和业务场景持续增加，接入、费用、安全与审计需要统一管理。")
    add_text(slide, 0.92, 1.48, 2.75, 0.28, "使用规模的变化", size=12, bold=True, color=MUTED)
    stages = [("员工使用 AI", "由个人试用扩展至日常业务场景", RED),
              ("模型和场景扩展", "跨部门、多场景并行使用", BLUE),
              ("企业统一治理", "统一管理身份、额度、用量及安全审计", RED)]
    for i, (head, body, accent) in enumerate(stages):
        y = 1.88 + i * 1.16
        add_box(slide, 0.92, y, 2.75, 0.72, head, LIGHT_RED if accent == RED else LIGHT_BLUE,
                accent, size=13.5, bold=True)
        add_text(slide, 0.98, y + 0.79, 2.62, 0.25, body, size=9.8, color=MUTED,
                 align=PP_ALIGN.CENTER)
        if i < len(stages) - 1:
            # Keep the stage connector in a dedicated gutter so it never crosses the captions.
            add_line(slide, 0.70, y + 0.72, 0.92, y + 0.72, BLUE)
            add_arrow(slide, 0.70, y + 0.72, 0.70, y + 1.16, BLUE)
            add_line(slide, 0.70, y + 1.16, 0.92, y + 1.16, BLUE)
    add_text(slide, 0.92, 5.58, 2.75, 0.58, "规模扩大后，分散管理将增加配置成本、费用失控和安全审计风险。",
             size=12.5, bold=True, color=INK, align=PP_ALIGN.CENTER)

    cards = [("接入方式不统一", "不同模型、不同接口并存，配置由各方维护，入口分散。", RED, LIGHT_RED),
             ("使用人数和场景增加", "跨部门、跨项目使用，Key 与使用者缺少明确关联。", BLUE, LIGHT_BLUE),
             ("费用逐渐增加", "Token 和费用缺少统一统计，超支与浪费难以及时识别。", RED, LIGHT_RED),
             ("安全要求更高", "凭证保护、使用记录和审计要求日益明确。", BLUE, LIGHT_BLUE)]
    for i, (head, body, accent, fill) in enumerate(cards):
        x = 4.05 + (i % 2) * 4.25
        y = 1.72 + (i // 2) * 2.12
        add_card(slide, x, y, 3.82, 1.72, head, body, accent, fill,
                 title_size=13.5, body_size=11.2)
    add_text(slide, 8.45, 5.98, 3.65, 0.42, "因此需要建设统一的接入与管理入口。",
             size=14, bold=True, color=RED, align=PP_ALIGN.CENTER)


def populate_background_problems(slide):
    clear_slide(slide)
    add_page_title(slide, "需求背景（二）｜企业 AI 使用中的四类管理问题",
                   "这些问题相互关联，直接影响使用安全、费用控制和责任追溯。")
    matrix = [("Key 归属不清", "Key 分散保存，缺少明确归属。\n影响：问题发生后难以明确责任人。", RED, LIGHT_RED),
              ("额度控制不足", "缺少统一的额度和费用规则。\n影响：增加资源浪费与费用超支风险。", BLUE, LIGHT_BLUE),
              ("用量统计不清", "缺少模型、部门等维度的用量数据。\n影响：资源配置与费用评估缺少依据。", RED, LIGHT_RED),
              ("风险不易发现", "风险请求与普通请求混合记录。\n影响：异常调用难以及时识别。", BLUE, LIGHT_BLUE)]
    for i, (head, body, accent, fill) in enumerate(matrix):
        x = 0.92 + (i % 2) * 6.08
        y = 1.68 + (i // 2) * 2.05
        add_card(slide, x, y, 5.45, 1.70, head, body, accent, fill,
                 title_size=14.5, body_size=11.8)
    add_box(slide, 4.52, 5.78, 4.25, 0.60, "统一平台集中治理", RED, RED,
            size=14, bold=True, color=WHITE)
    add_text(slide, 1.05, 6.52, 11.1, 0.28, "从身份、额度、用量到风险，形成统一管理、查询和追溯链路。",
             size=13.5, bold=True, color=INK, align=PP_ALIGN.CENTER)


def populate_architecture(slide):
    clear_slide(slide)
    add_page_title(slide, "总体架构｜统一接入、统一管理、统一审计")
    add_box(slide, 0.82, 1.65, 1.85, 0.72, "客户端 / Agent", PALE, RED, size=14, bold=True)
    add_box(slide, 3.05, 1.65, 2.05, 0.72, "CLIProxyAPI\n统一接入", RED, RED, size=14, bold=True, color=WHITE)
    add_box(slide, 5.55, 1.65, 2.25, 0.72, "Management Center\n统一管理", BLUE, BLUE, size=14, bold=True, color=WHITE)
    add_box(slide, 5.55, 3.00, 2.25, 0.72, "Usage Service\n采集 · 统计 · 联动", PALE, BLUE, size=13.5, bold=True)
    add_box(slide, 8.35, 1.65, 2.20, 0.72, "插件 / 审计\n受控扩展", PALE, RED, size=13.5, bold=True)
    add_arrow(slide, 2.67, 2.01, 3.05, 2.01, RED)
    add_arrow(slide, 5.10, 2.01, 5.55, 2.01, BLUE)
    add_arrow(slide, 6.68, 2.37, 6.68, 3.00, BLUE)
    add_arrow(slide, 7.80, 2.01, 8.35, 2.01, RED)
    add_card(slide, GRID_3_XS[0], 4.35, GRID_3_W, 1.35, "接入", "统一接入不同模型服务，提供一致的调用入口，减少重复配置。", RED, PALE, body_size=12)
    add_card(slide, GRID_3_XS[1], 4.35, GRID_3_W, 1.35, "管理", "集中管理 Key、额度和用量，记录处理结果，支持查询和恢复。", BLUE, PALE, body_size=12)
    add_card(slide, GRID_3_XS[2], 4.35, GRID_3_W, 1.35, "安全审查", "检查插件来源、访问路径和可查看资源，保护凭证和请求记录。", RED, PALE, body_size=12)


def contribution_page(slide, title, subtitle, flow_labels, cards, footer, variant="flow"):
    clear_slide(slide)
    add_page_title(slide, title, subtitle)

    if variant == "vertical":
        # Identity uses a left-to-right reading path with a vertical operation list.
        add_text(slide, 0.92, 1.42, 2.3, 0.25, "使用流程", size=11.5, bold=True, color=MUTED)
        for i, label in enumerate(flow_labels):
            y = 1.78 + i * 0.82
            add_box(slide, 0.92, y, 2.35, 0.58, label, PALE if i != 1 else RED,
                    BLUE if i != 1 else RED, size=13, bold=True,
                    color=INK if i != 1 else WHITE)
            if i < len(flow_labels) - 1:
                add_arrow(slide, 2.095, y + 0.58, 2.095, y + 0.82, BLUE)
        add_card(slide, 3.65, 1.62, 4.05, 2.18, cards[0][0], cards[0][1],
                 cards[0][2], LIGHT_RED if cards[0][2] == RED else LIGHT_BLUE,
                 title_size=15, body_size=12.0)
        add_card(slide, 8.05, 1.62, 4.05, 2.18, cards[1][0], cards[1][1],
                 cards[1][2], LIGHT_RED if cards[1][2] == RED else LIGHT_BLUE,
                 title_size=15, body_size=12.0)
        add_card(slide, 3.65, 4.20, 8.45, 1.42, cards[2][0], cards[2][1],
                 cards[2][2], LIGHT_RED if cards[2][2] == RED else LIGHT_BLUE,
                 title_size=15, body_size=12.0)
        add_text(slide, 3.65, 5.88, 8.45, 0.35, footer, size=14, bold=True,
                 color=INK, align=PP_ALIGN.CENTER)
        return

    if variant == "compare":
        # Quota governance is easier to read as a before/after comparison.
        add_card(slide, 0.92, 1.58, 5.15, 1.68, "使用前｜" + cards[0][0], cards[0][1],
                 RED, LIGHT_RED, title_size=15, body_size=12.0)
        add_card(slide, 7.25, 1.58, 5.15, 1.68, "平台处理｜" + cards[1][0], cards[1][1],
                 BLUE, LIGHT_BLUE, title_size=15, body_size=12.0)
        add_arrow(slide, 6.18, 2.42, 7.08, 2.42, RED, width=2.2)
        add_text(slide, 5.98, 1.98, 1.2, 0.26, "统一规则", size=11, bold=True,
                 color=RED, align=PP_ALIGN.CENTER)
        add_flow(slide, flow_labels, x=0.92, y=3.72, box_w=2.42, gap=0.40)
        add_card(slide, 0.92, 4.82, 11.48, 1.18, cards[2][0], cards[2][1],
                 cards[2][2], LIGHT_RED if cards[2][2] == RED else LIGHT_BLUE,
                 title_size=15, body_size=12.0)
        add_text(slide, 0.92, 6.23, 11.48, 0.30, footer, size=14, bold=True,
                 color=INK, align=PP_ALIGN.CENTER)
        return

    if variant == "dashboard":
        # Analytics uses metric-like blocks instead of another row of explanatory cards.
        labels = [("记录请求", "每次调用"), ("统计汇总", "按维度"),
                  ("趋势排名", "看变化"), ("查看明细", "查记录")]
        for i, (head, sub) in enumerate(labels):
            x = 0.92 + i * 3.05
            add_box(slide, x, 1.58, 2.62, 0.86, head + "\n" + sub,
                    RED if i == 1 else PALE, RED if i == 1 else BLUE,
                    size=13, bold=True, color=WHITE if i == 1 else INK)
        add_card(slide, 0.92, 2.92, 5.45, 2.22, cards[0][0], cards[0][1],
                 cards[0][2], LIGHT_RED if cards[0][2] == RED else LIGHT_BLUE,
                 title_size=15, body_size=12.0)
        add_card(slide, 6.78, 2.92, 5.62, 2.22, cards[1][0], cards[1][1],
                 cards[1][2], LIGHT_RED if cards[1][2] == RED else LIGHT_BLUE,
                 title_size=15, body_size=12.0)
        add_card(slide, 0.92, 5.45, 11.48, 0.74, cards[2][0], cards[2][1],
                 cards[2][2], LIGHT_RED if cards[2][2] == RED else LIGHT_BLUE,
                 title_size=14, body_size=11.3)
        return

    if variant == "risk":
        # Security uses a risk-handling loop rather than the standard 4-step row.
        add_box(slide, 0.95, 1.82, 2.35, 0.82, flow_labels[0], PALE, BLUE,
                size=14, bold=True)
        add_box(slide, 4.32, 1.62, 4.40, 1.22, "cyber_policy\n风险标记", RED, RED,
                size=16, bold=True, color=WHITE)
        add_box(slide, 9.72, 1.82, 2.35, 0.82, flow_labels[2], PALE, BLUE,
                size=14, bold=True)
        add_arrow(slide, 3.30, 2.23, 4.32, 2.23, BLUE, width=2.0)
        add_arrow(slide, 8.72, 2.23, 9.72, 2.23, BLUE, width=2.0)
        add_text(slide, 4.48, 3.05, 4.05, 0.28, "先标记，再检查，最后处理", size=12, bold=True,
                 color=RED, align=PP_ALIGN.CENTER)
        add_card(slide, 0.92, 3.55, GRID_3_W, 1.76, cards[0][0], cards[0][1],
                 cards[0][2], LIGHT_RED if cards[0][2] == RED else LIGHT_BLUE,
                 title_size=15, body_size=12.0)
        add_card(slide, 4.70, 3.55, GRID_3_W, 1.76, cards[1][0], cards[1][1],
                 cards[1][2], LIGHT_RED if cards[1][2] == RED else LIGHT_BLUE,
                 title_size=15, body_size=12.0)
        add_card(slide, 8.48, 3.55, GRID_3_W, 1.76, cards[2][0], cards[2][1],
                 cards[2][2], LIGHT_RED if cards[2][2] == RED else LIGHT_BLUE,
                 title_size=15, body_size=12.0)
        add_text(slide, 0.92, 5.78, 11.48, 0.36, footer, size=14, bold=True,
                 color=INK, align=PP_ALIGN.CENTER)
        return

    if variant == "entry":
        # Unified entry is presented as a before/after comparison for employees.
        add_card(slide, 0.92, 1.65, 4.65, 1.95, "使用前｜分散配置", cards[0][1],
                 RED, LIGHT_RED, title_size=15, body_size=12.0)
        add_card(slide, 7.78, 1.65, 4.62, 1.95, "建设后｜统一入口", cards[1][1],
                 BLUE, LIGHT_BLUE, title_size=15, body_size=12.0)
        add_arrow(slide, 5.72, 2.62, 7.53, 2.62, RED, width=2.2)
        add_text(slide, 5.84, 2.12, 1.55, 0.3, "接入统一", size=11.5, bold=True,
                 color=RED, align=PP_ALIGN.CENTER)
        add_flow(slide, ["员工 / Agent", "CLIProxyAPI", "统一调用", "集中管理"],
                 x=1.05, y=4.18, box_w=2.42, gap=0.40)
        add_card(slide, 0.92, 5.42, 11.48, 0.82, "建设效果", cards[2][1],
                 RED, LIGHT_RED, title_size=13.5, body_size=11.2)
        return

    if variant == "quota_decision":
        # Quota governance is shown as a rule-decision path rather than a card row.
        add_card(slide, 0.92, 1.62, 3.20, 4.48, cards[0][0], cards[0][1],
                 RED, LIGHT_RED, title_size=15, body_size=12.0)
        add_text(slide, 4.58, 1.45, 7.6, 0.28, "额度判断路径", size=12, bold=True, color=MUTED)
        add_box(slide, 4.58, 1.88, 2.72, 0.68, "默认限额", PALE, BLUE, size=13.5, bold=True)
        add_box(slide, 8.50, 1.88, 2.72, 0.68, "Key 单独设置", PALE, BLUE, size=13.5, bold=True)
        # Two rule sources join a shared bus before the common usage calculation.
        add_line(slide, 5.94, 2.56, 5.94, 2.72, BLUE)
        add_line(slide, 9.86, 2.56, 9.86, 2.72, BLUE)
        add_line(slide, 5.94, 2.72, 9.86, 2.72, BLUE)
        add_box(slide, 6.50, 3.00, 2.82, 0.68, "记录 Token / 费用", PALE, BLUE, size=13.2, bold=True)
        add_arrow(slide, 7.91, 2.72, 7.91, 3.00, BLUE)
        add_box(slide, 6.50, 4.10, 2.82, 0.68, "超过限额", RED, RED, size=13.5, bold=True, color=WHITE)
        add_arrow(slide, 7.91, 3.68, 7.91, 4.10, RED)
        add_box(slide, 4.58, 5.02, 2.10, 0.62, "暂停", PALE, RED, size=13, bold=True)
        add_box(slide, 7.00, 5.02, 2.10, 0.62, "降级", PALE, BLUE, size=13, bold=True)
        add_box(slide, 9.42, 5.02, 2.10, 0.62, "告警", PALE, RED, size=13, bold=True)
        # A clean branch bus avoids diagonal lines crossing the action labels.
        add_line(slide, 7.91, 4.78, 7.91, 4.92, RED)
        add_line(slide, 5.63, 4.92, 10.47, 4.92, RED)
        add_arrow(slide, 5.63, 4.92, 5.63, 5.02, RED)
        add_arrow(slide, 8.05, 4.92, 8.05, 5.02, BLUE)
        add_arrow(slide, 10.47, 4.92, 10.47, 5.02, RED)
        add_card(slide, 4.58, 5.86, 6.94, 0.78, "规则结果", cards[2][1],
                 RED, LIGHT_RED, title_size=12.5, body_size=10.4)
        return

    # Default: the compact four-step flow remains useful for the unified-entry page.
    add_flow(slide, flow_labels, x=1.15, y=1.60, box_w=2.42, gap=0.40)
    for i, (head, body, accent) in enumerate(cards):
        x = GRID_3_XS[i]
        add_card(slide, x, 2.95, GRID_3_W, 2.10, head, body, accent,
                 LIGHT_RED if accent == RED else LIGHT_BLUE, title_size=14, body_size=12.0)
    add_text(slide, 1.05, 5.60, 11.2, 0.40, footer, size=15, bold=True, color=INK, align=PP_ALIGN.CENTER)


def landing_page(slide, title, subtitle, cards, code_heading, code, note,
                 images, code_x=0.82, code_w=5.9, variant="standard"):
    clear_slide(slide)
    add_page_title(slide, title, subtitle)

    if variant == "key_annotated":
        # Enterprise Key is screenshot-led, with three focused annotations on the left.
        y_positions = [1.62, 2.92, 4.22]
        for (head, body, accent), y in zip(cards, y_positions):
            add_card(slide, 0.92, y, 4.95, 1.08, head, body, accent,
                     LIGHT_RED if accent == RED else LIGHT_BLUE,
                     title_size=13.2, body_size=10.6)
        add_code_panel(slide, 0.92, 5.56, 4.95, 0.98, code_heading, code, RED, 8.2)
        add_picture(slide, images[0], 6.28, 1.62, 5.92, 4.88, line=BLUE)
        add_text(slide, 6.35, 6.62, 5.75, 0.32, note, size=10.8, bold=True,
                 color=INK, align=PP_ALIGN.CENTER)
        return

    if variant == "usage_pipeline":
        # Usage statistics combines a data path, a primary result view, and a small evidence panel.
        add_card(slide, 0.92, 1.58, 5.00, 1.12, cards[0][0], cards[0][1],
                 cards[0][2], LIGHT_RED if cards[0][2] == RED else LIGHT_BLUE,
                 title_size=13.5, body_size=10.8)
        add_box(slide, 0.92, 3.02, 1.48, 0.68, "usage_events", PALE, RED, size=11.5, bold=True)
        add_box(slide, 2.67, 3.02, 1.48, 0.68, "按时间汇总", PALE, BLUE, size=11.5, bold=True)
        add_box(slide, 4.42, 3.02, 1.48, 0.68, "明细分页", PALE, RED, size=11.5, bold=True)
        add_arrow(slide, 2.40, 3.36, 2.62, 3.36, BLUE)
        add_arrow(slide, 4.15, 3.36, 4.37, 3.36, BLUE)
        add_card(slide, 0.92, 4.02, 5.00, 1.18, cards[1][0], cards[1][1],
                 cards[1][2], LIGHT_RED if cards[1][2] == RED else LIGHT_BLUE,
                 title_size=13.5, body_size=10.8)
        add_code_panel(slide, 0.92, 5.52, 5.00, 0.98, code_heading, code, RED, 8.2)
        add_picture(slide, images[1], 6.25, 1.58, 6.05, 3.78, line=BLUE)
        add_picture(slide, images[0], 6.25, 5.52, 2.82, 1.28, line=RED)
        add_card(slide, 9.34, 5.52, 2.96, 1.28, cards[2][0], cards[2][1],
                 cards[2][2], LIGHT_RED if cards[2][2] == RED else LIGHT_BLUE,
                 title_size=12.6, body_size=10.1)
        return

    for i, (head, body, accent) in enumerate(cards):
        x = GRID_3_XS[i]
        add_card(slide, x, 1.50, GRID_3_W, 1.78, head, body, accent,
                 LIGHT_RED if accent == RED else LIGHT_BLUE, title_size=14, body_size=11.8)
    add_code_panel(slide, code_x, 3.52, code_w, 1.10, code_heading, code, RED, 9.0)
    add_text(slide, code_x + code_w + 0.35, 3.70, 11.6 - code_w - code_x - 0.35, 0.85, note,
             size=12.5, bold=True, color=INK, align=PP_ALIGN.LEFT)
    if len(images) == 1:
        add_picture(slide, images[0], SINGLE_IMAGE_X, 4.85, SINGLE_IMAGE_W, 2.10, line=BLUE)
    else:
        add_picture(slide, images[0], GRID_2_XS[0], 4.85, GRID_2_W, 2.10, line=BLUE)
        add_picture(slide, images[1], GRID_2_XS[1], 4.85, GRID_2_W, 2.10, line=RED)


def populate_summary(slide):
    clear_slide(slide)
    add_page_title(slide, "总结｜形成企业 AI 使用管理体系",
                   "统一接入，统一管理身份、额度、用量和安全。")
    add_card(slide, GRID_3_XS[0], 1.62, GRID_3_W, 1.72, "统一入口", "不同模型通过同一个入口接入，减少重复配置。", RED, LIGHT_RED, title_size=15, body_size=12.0)
    add_card(slide, GRID_3_XS[1], 1.62, GRID_3_W, 1.72, "统一管理", "集中管理 Key、额度和使用情况，责任清楚、费用可控。", BLUE, LIGHT_BLUE, title_size=15, body_size=12.0)
    add_card(slide, GRID_3_XS[2], 1.62, GRID_3_W, 1.72, "数据可查", "请求、Token、费用和趋势都可查询，使用情况更清楚。", RED, LIGHT_RED, title_size=15, body_size=12.0)
    add_card(slide, GRID_3_XS[0], 3.55, GRID_3_W, 1.72, "风险可查", "风险请求单独标记，支持查看脱敏内容和人工检查。", BLUE, LIGHT_BLUE, title_size=15, body_size=12.0)
    add_card(slide, GRID_3_XS[1], 3.55, GRID_3_W, 1.72, "便于维护", "统计结果可以重算，已有接入保持兼容，功能便于持续维护。", RED, LIGHT_RED, title_size=15, body_size=12.0)
    add_card(slide, GRID_3_XS[2], 3.55, GRID_3_W, 1.72, "实际使用", "平台已完成验证并推广使用，支持企业员工规范使用 AI。", BLUE, LIGHT_BLUE, title_size=15, body_size=12.0)
    add_text(slide, 1.05, 5.75, 11.2, 0.42, "从统一接入到统一治理，形成有记录、有规则、可追溯的 AI 使用管理链路。",
             size=15, bold=True, color=INK, align=PP_ALIGN.CENTER)


def build():
    if not TEMPLATE.exists():
        raise FileNotFoundError(TEMPLATE)
    prs = Presentation(str(TEMPLATE))
    if len(prs.slides) != 8:
        raise RuntimeError(f"template must have 8 slides, got {len(prs.slides)}")

    cover = prs.slides[0]
    group = cover.shapes[3]
    replace_all_runs(group.shapes[1], {"XXX": "CLIProxyAPI 企业级 AI 网关建设"})
    replace_all_runs(group.shapes[2], {"XXX": "吴强辉"})

    contents = prs.slides[1]
    set_plain(contents.shapes[0], "个人概况", size=20, bold=True, color=INK)
    set_plain(contents.shapes[4], "申报亮点", size=20, bold=True, color=INK)
    set_plain(contents.shapes[7], "AI 贡献点", size=19, bold=True, color=INK)
    set_plain(contents.shapes[10], "总结", size=20, bold=True, color=INK)

    populate_profile(prs.slides[2])
    populate_highlights(prs.slides[3])
    populate_background_phase(prs.slides[4])
    populate_background_problems(prs.slides[5])
    populate_architecture(prs.slides[6])
    thanks_sld_id = prs.slides._sldIdLst[7]

    # AI 贡献点
    contribution_page(new_inner_slide(prs),
        "贡献点一｜统一入口，减少重复配置",
        "把不同模型的接入方式集中管理，员工使用同一个入口。",
        ["统一接入", "统一配置", "统一访问", "集中管理"],
        [
            ("现状", "不同员工和系统分别配置模型，入口不统一，故障定位与责任追溯较为困难。", RED),
            ("实现", "提供统一的接入和配置入口，配置集中管理，调用通过统一代理和认证。", BLUE),
            ("作用", "减少重复配置，降低维护成本，后续 Key、额度和用量统计也有统一入口。", RED),
        ],
        "统一模型接入方式，再集中管理使用情况。", variant="entry")

    contribution_page(new_inner_slide(prs),
        "贡献点二｜身份绑定，明确使用归属",
        "把 Key 与使用人、部门和来源关联起来，方便查询和追溯。",
        ["批量导入", "预览确认", "绑定身份", "查询记录"],
        [
            ("现状", "Key 缺少明确归属，跨部门使用时难以定位责任人，问题发生后也难以追溯。", RED),
            ("实现", "支持批量导入和 CSV 预览，把 Key 与人员、部门、来源和导入时间关联，并保留历史记录。", BLUE),
            ("作用", "查询用量或审计时，可以按人员和部门找到对应记录。", RED),
        ],
        "建立 Key 与人员、部门的关联，为统计和审计提供依据。", variant="vertical")

    contribution_page(new_inner_slide(prs),
        "贡献点三｜统一额度，避免超支和浪费",
        "统一统计 Token 和费用，超过限额后按规则处理。",
        ["记录用量", "计算费用", "匹配规则", "执行处理"],
        [
            ("现状", "费用缺少统一统计，超限后才处置，增加资源浪费和费用超支风险。", RED),
            ("实现", "统一统计 cost 和 Token，支持默认限额和 Key 单独设置；超过限额后可暂停、降级或告警，异常时不擅自改变状态。", BLUE),
            ("作用", "管理员能看到费用和使用情况，超限有明确处理方式，减少无效消耗。", RED),
        ],
        "把额度规则提前设好，超过限额后按规则处理。", variant="quota_decision")

    contribution_page(new_inner_slide(prs),
        "贡献点四｜统一统计，掌握 AI 使用情况",
        "按模型、部门、Key 查看用量、费用和变化趋势。",
        ["记录请求", "统计汇总", "趋势排名", "查看明细"],
        [
            ("现状", "缺少模型、部门和 Key 等维度的数据，难以判断用量与费用变化。", RED),
            ("实现", "记录请求和费用，按模型、部门等维度统计，支持趋势、排行和明细查询。", BLUE),
            ("作用", "为选择模型、安排资源和评估费用提供清晰数据。", RED),
        ],
        "统一统计请求、用量和费用，为资源投入评估提供依据。", variant="dashboard")

    contribution_page(new_inner_slide(prs),
        "贡献点五｜发现风险请求，支持人工检查",
        "对标记为 cyber_policy 的请求单独记录，方便人工检查。",
        ["记录请求", "标记风险", "人工检查", "保护账号"],
        [
            ("现状", "风险请求与普通请求混合记录，异常调用难以及时识别。", RED),
            ("实现", "命中 cyber_policy 的请求会被标记并计数；管理员可查看脱敏后的请求内容、账号和调用信息，人工判断是否异常。", BLUE),
            ("作用", "先筛出高风险调用，再决定是否处理，减少账号、额度和凭证被误用造成的损失。", RED),
        ],
        "单独标记风险请求，结合人工检查降低异常调用风险。", variant="risk")

    # 功能落地
    landing_page(new_inner_slide(prs),
        "落地｜企业 Key 管理",
        "从导入 Key 到绑定人员和部门，支持查询和安全展示。",
        [
            ("批量导入与预览", "支持 CSV 导入和预览确认，部门、Key 和错误原因逐项展示。", RED),
            ("绑定使用人和部门", "用稳定标识关联使用人、部门、来源和导入时间，不改变原有接入方式。", BLUE),
            ("记录可查且不泄露", "导入结果和历史记录可复核，页面只展示掩码、Hash 等非敏感信息。", RED),
        ],
        "Key 关联方式（示意）",
        "interface EnterpriseKeyBinding {\n  apiKey: string\n  apiKeyHash: string // 稳定标识\n}",
        "用稳定标识找到对应记录，原始 Key 不直接展示或用于统计。",
        [ROOT / "media" / "account-overview.png"], variant="key_annotated")

    landing_page(new_inner_slide(prs),
        "落地｜额度管理",
        "统一统计使用量，超过限额按规则处理，状态变更有明确依据。",
        [
            ("统一统计", "统一统计 cost 和 Token，按日、周等时间范围查看使用情况。", RED),
            ("规则处理", "支持默认限额和 Key 单独设置；超过限额后可暂停、降级或告警。", BLUE),
            ("异常保护", "上游状态无法确认时保留当前状态并记录原因，避免误操作。", RED),
        ],
        "限额判断（示意）",
        "func (c SpendLimitConfig) LimitForKey(hash string) SpendLimit {\n  if l, ok := c.OverrideForKey(hash); ok { return l }\n  return c.Default\n}",
        "先查 Key 的单独设置，没有时使用默认限额，处理规则清楚可查。",
        [ROOT / "media" / "quota-management.png", ROOT / "media" / "credential-quota.png"])

    landing_page(new_inner_slide(prs),
        "落地｜用量统计",
        "保留原始记录，统计结果可重算，明细查询稳定。",
        [
            ("保留原始记录", "usage_events 保存每次请求记录，统计结果可以重新计算和核对。", RED),
            ("快速汇总", "按小时、按天和不同维度汇总；数据不完整时回到原始记录并标明情况。", BLUE),
            ("明细可追溯", "按时间和编号分页，保证记录不重复、不遗漏，并返回真实总数。", RED),
        ],
        "查询条件（示意）",
        "type AnalyticsQuery struct {\n  Include []string\n  Cursor  *Cursor\n}",
        "按需要返回统计结果，明细分页稳定，数据来源和范围清楚。",
        [ROOT / "media" / "request-monitoring.png", ROOT / "media" / "request-monitoring-detail.png"], variant="usage_pipeline")

    landing_page(new_inner_slide(prs),
        "落地｜风险请求标记与人工检查",
        "通过 cyber_policy 标记风险请求，支持人工查看请求文本和调用信息。",
        [
            ("标记风险请求", "命中 cyber_policy 的请求记录风险信号并计数，方便筛选重点请求。", RED),
            ("人工检查", "管理员查看脱敏后的用户请求内容、账号和调用信息，判断是否存在异常。", BLUE),
            ("减少资产损失", "发现高风险调用后及时处理，降低账号、额度和凭证被误用的风险。", RED),
        ],
        "风险标记（示意）",
        "const SecuritySignalCyberPolicy = \"cyber_policy\"\nif event.SecuritySignal == SecuritySignalCyberPolicy {\n  payload.SecuritySignalCount++\n}",
        "系统单独统计风险请求，管理员按标记筛选，再结合脱敏内容人工检查。",
        [ROOT / "media" / "inspection.png", ROOT / "media" / "inspection-policy.png"])

    populate_summary(new_inner_slide(prs))

    # 移动模板结尾页到最后
    sld_id_lst = prs.slides._sldIdLst
    sld_id_lst.remove(thanks_sld_id)
    sld_id_lst.append(thanks_sld_id)

    if OUTPUT.exists(): OUTPUT.unlink()
    prs.save(str(OUTPUT))
    shutil.copy2(OUTPUT, CURRENT)
    shutil.copy2(OUTPUT, NAMED)
    print(f"created {OUTPUT}")
    print(f"updated {CURRENT}")
    print(f"updated {NAMED}")
    print(f"slides {len(prs.slides)}")


if __name__ == "__main__":
    build()
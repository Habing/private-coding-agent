"""Quick check: login, open workflow designer, screenshot canvas."""
from playwright.sync_api import sync_playwright

BASE = "http://localhost:8080"

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page(viewport={"width": 1400, "height": 900})
    page.goto(f"{BASE}/login")
    page.get_by_label("租户").fill("default")
    page.get_by_label("邮箱").fill("demo@example.com")
    page.get_by_label("密码").fill("demo123")
    page.get_by_role("button", name="登录").click()
    page.wait_for_url(f"{BASE}/**", timeout=15000)

    page.goto(f"{BASE}/workflows")
    page.wait_for_load_state("networkidle")
    page.get_by_role("button", name="我的工作流").click()
    page.wait_for_timeout(1500)

    edit_btn = page.get_by_role("button", name="编辑").first
    if edit_btn.count() == 0:
        page.screenshot(path="debug-workflows-no-edit.png", full_page=True)
        raise SystemExit("no workflow edit button found")

    edit_btn.click()
    page.get_by_role("button", name="设计器").click()
    page.wait_for_timeout(3000)

    canvas = page.locator(".workflow-builder-embed")
    canvas.wait_for(timeout=20000)
    canvas.screenshot(path="debug-workflow-canvas.png")

    ro = page.locator('[aria-label*="read" i], [title*="read" i]').count()
    nodes = page.locator(".react-flow__node").count()
    print(f"nodes_on_canvas={nodes}")
    page.screenshot(path="debug-workflow-designer-full.png", full_page=True)
    browser.close()
    print("saved debug-workflow-canvas.png and debug-workflow-designer-full.png")

"""Quick check: login, open SWD workflow designer, select a step, screenshot."""
from __future__ import annotations

import os
import sys

from playwright.sync_api import sync_playwright

BASE = os.environ.get("PCA_E2E_BASE_URL", "http://localhost:8080")
SLUG = os.environ.get("PCA_E2E_WORKFLOW_SLUG", "e2e-mock-chain")
TENANT = os.environ.get("PCA_E2E_TENANT", "default")
EMAIL = os.environ.get("PCA_E2E_EMAIL", "demo@example.com")
PASSWORD = os.environ.get("PCA_E2E_PASSWORD", "demo123")


def main() -> None:
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page(viewport={"width": 1400, "height": 900})
        page.goto(f"{BASE}/login")
        page.get_by_label("租户").fill(TENANT)
        page.get_by_label("邮箱").fill(EMAIL)
        page.get_by_label("密码").fill(PASSWORD)
        page.get_by_role("button", name="登录").click()
        page.wait_for_url(f"{BASE}/**", timeout=15000)

        page.goto(f"{BASE}/workflows")
        page.wait_for_load_state("networkidle")
        page.get_by_role("button", name="我的工作流").click()

        row = page.locator("li").filter(has_text=SLUG)
        if row.count() == 0:
            page.screenshot(path="debug-workflows-no-edit.png", full_page=True)
            browser.close()
            sys.exit(f"workflow slug not found: {SLUG}")

        row.get_by_role("button", name="编辑").click()
        page.get_by_role("button", name="设计器").click()

        canvas = page.get_by_test_id("workflow-swd-canvas").or_(page.locator(".swd-embed"))
        canvas.wait_for(timeout=25000)
        canvas.locator(".sqd-designer").wait_for(timeout=25000)

        status = page.get_by_text("status · fetch_status", exact=True)
        status.wait_for(timeout=15000)
        status.click()

        page.get_by_text("在左侧画布中选中一步").wait_for(state="hidden", timeout=10000)

        canvas.screenshot(path="debug-workflow-canvas.png")
        page.screenshot(path="debug-workflow-designer-full.png", full_page=True)
        browser.close()
        print("saved debug-workflow-canvas.png and debug-workflow-designer-full.png")


if __name__ == "__main__":
    main()

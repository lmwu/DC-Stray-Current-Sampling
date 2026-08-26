using Pkg
Pkg.add("PackageCompiler")

using PackageCompiler

# 開始編譯應用程式
PackageCompiler.create_app(
    ".",       # 原始專案路徑
    "build_server",         # 輸出資料夾名稱
    force=true,             # 若資料夾已存在則覆蓋
    incremental=false       # 完全編譯（編譯時間較長，但執行速度最快）
)
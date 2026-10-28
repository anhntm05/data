# data - Tool hỗ trợ QA test data

| Tool | Làm gì | Hướng dẫn |
|---|---|---|
| `comp` (trước đây là `uatdiff`) | So sánh 2 file CSV UAT và PROD theo key, xuất Excel/CSV có tô màu, lấy note từ file reference | [comp/README.md](comp/README.md) |
| `insertgen` | Sinh câu lệnh INSERT từ DDL + CSV cho Oracle (DBeaver) hoặc Athena | [insertgen/README.md](insertgen/README.md) |

Cả hai là 1 file `.exe` cho Windows, không cần cài Go, Node hay Python.

## Lấy bản mới nhất
Double-click `update_tools.bat`: tải `comp.exe` và `insertgen.exe` mới nhất về cùng thư mục.
Mỗi lần code trên `main` thay đổi, GitHub Actions tự test, build và cập nhật 2 file này ở release
[latest](https://github.com/anhntm05/data/releases/tag/latest).

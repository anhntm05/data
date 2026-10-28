# data - Tool hỗ trợ QA test data

| Tool | Làm gì | Hướng dẫn |
|---|---|---|
| `comp` (trước đây là `uatdiff`) | So sánh 2 file CSV UAT và PROD theo key, xuất Excel/CSV có tô màu, lấy note từ file reference | [comp/README.md](comp/README.md) |

Tool là 1 file `.exe` cho Windows, không cần cài Go, Node hay Python.

## Build
Mỗi tool tự build trong thư mục của nó: double-click `build.bat` (Windows) hoặc chạy `./build.sh` (Mac/Linux) trên máy có Go,
rồi chép file `.exe` sang máy công ty. Xem phần "Build từ source" trong README của từng tool.

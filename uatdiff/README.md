# uatdiff - So sánh dữ liệu UAT vs PROD

Tool CLI (1 file .exe, không cần cài môi trường) so sánh 2 file CSV export từ UAT và PROD, xuất Excel/CSV có định dạng.

## Chạy
- Cách 1: double-click `uatdiff.exe` (nên đặt exe cùng thư mục với các file CSV, tool sẽ liệt kê sẵn).
- Cách 2: chọn 2 file CSV trong Explorer, kéo thả lên `uatdiff.exe` (tool hỏi file nào là UAT/PROD).
- Cách 3: mở Windows Terminal / cmd, gõ `uatdiff.exe` hoặc `uatdiff.exe UAT.csv PROD.csv`.

Nếu Windows SmartScreen cảnh báo: More info → Run anyway.

## Phím
↑/↓ chọn, Space tick, Enter xác nhận, / tìm theo tên, ←/→ hoặc y/n cho câu hỏi Có/Không, Ctrl+C thoát.
Ô nhập đường dẫn: kéo file từ Explorer thả vào cửa sổ terminal.

## Luồng
1. Chọn file UAT, PROD → hiện số dòng, cột chung, cảnh báo cột lệch tên (không được dùng).
2. Nếu có ngày dạng 03/04/2026 → chọn dd/mm hay mm/dd.
3. Chọn cột key (1 hoặc nhiều), sắp lại thứ tự key nếu cần.
4. Nếu key trùng trong 1 file → hiện cột khác nhau giữa các dòng trùng, gợi ý thêm key; hoặc tiếp tục (ghép theo thứ tự xuất hiện).
5. Chọn cột so sánh (mặc định tất cả, bỏ tick cột muốn bỏ qua).
6. File reference (.xlsx/.csv) để lấy note theo key.
7. Sắp xếp: mặc định environment (Chỉ PROD → Chỉ UAT → Cả 2), note Z-A, note trống ở cuối; thêm cột tuỳ ý.
8. Định dạng: xlsx / csv / cả hai → xác nhận → chạy.

## Quy tắc so sánh
- Bỏ khoảng trắng đầu/cuối; phân biệt hoa thường; NULL = rỗng = ô trống.
- Số so sánh chính xác dạng thập phân: 1.0 = 1, 12435678654.04001 ≠ 12435678654.03001. Số có số 0 ở đầu (007) coi là chuỗi.
- Ngày quy về yyyy-mm-dd hoặc yyyy-mm-dd hh:mm:ss (nhận: 2026-10-26, 2026/10/26, 26/10/2026, 26-OCT-26, có giờ, AM/PM). 00:00:00 coi như chỉ có ngày.
- Note từ reference chỉ lấy cho dòng có khác biệt hoặc chỉ có ở 1 bên. Header reference được tự tìm (dòng có cột note và nhiều cột key nhất). Nếu reference thiếu một số cột key, tool cảnh báo ngay khi chọn file và khớp note theo các cột key còn lại.

## File kết quả
- 5 dòng tóm tắt, header ở dòng 7. Cột: key → environment → a_uat, a_prod, ... → Khac biet → note.
- Excel: mọi ô là Text (Excel không tự đổi số/ngày), tô màu theo environment, ô khác bôi đỏ, freeze header + cột key + environment, bật filter.
- CSV: UTF-8 BOM; giá trị có chữ số được ghi dạng ="..." để Excel mở ra vẫn là chuỗi.
- File kết quả cũ dùng lại được làm reference cho lần sau.

## Build lại từ source (máy có Go)
```
go test ./...
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o uatdiff.exe .
```
Trên Windows (PowerShell): `$env:GOOS="windows"; go build -ldflags "-s -w" -o uatdiff.exe .`

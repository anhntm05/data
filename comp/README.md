# comp - So sánh dữ liệu

Tool CLI (1 file .exe, không cần cài môi trường) so sánh 2 file CSV hoặc Excel (.xlsx) (file 1 và file 2, ví dụ export từ UAT và PROD), xuất file Excel có 3 tab.

## Chạy
- Cách 1: double-click `comp.exe` (nên đặt exe cùng thư mục với các file CSV, tool sẽ liệt kê sẵn).
- Cách 2: chọn 2 file CSV trong Explorer, kéo thả lên `comp.exe` (tool hỏi file nào là file 1, file nào là file 2).
- Cách 3: mở Windows Terminal / cmd, gõ `comp.exe` hoặc `comp.exe file1.csv file2.csv`.

Nếu Windows SmartScreen cảnh báo: More info → Run anyway.

## Phím
↑/↓ chọn, Space tick, Enter xác nhận, / tìm theo tên, Esc quay lại (ở bước chọn cột so sánh và chọn file reference), ←/→ hoặc y/n cho câu hỏi Có/Không, Ctrl+C thoát.
Ô nhập đường dẫn: kéo file từ Explorer thả vào cửa sổ terminal.

## Luồng
0. Chọn loại so sánh: "Dữ liệu UAT vs PROD" hoặc "Khác" (vd: 2 file phân tích khác biệt). Luồng "Khác" cho chọn file reference áp dụng cho dòng nào, xem bước 6.
1. Chọn file 1, file 2 (.csv hoặc .xlsx). Nếu file xlsx có nhiều sheet thì chọn sheet. Tool hiện số dòng, cột chung, cảnh báo cột lệch tên (không được dùng).
2. Nếu có ngày dạng 03/04/2026 → chọn dd/mm hay mm/dd.
3. Chọn cột key (1 hoặc nhiều), sắp lại thứ tự key nếu cần.
4. Nếu key trùng trong 1 file → hiện cột khác nhau giữa các dòng trùng, gợi ý thêm key; hoặc tiếp tục (ghép theo thứ tự xuất hiện).
5. Chọn cột so sánh (mặc định tất cả, bỏ tick cột muốn bỏ qua). Bấm Esc để quay lại chọn key.
6. File reference (.xlsx/.csv) để lấy note theo key. Với mỗi tab `records` và `field_mismatch`: nếu file reference có sheet cùng tên thì tự dùng; nếu không thì chọn sheet. Sau đó chọn cột chứa note và tên cột sẽ ghi note vào output (mặc định trùng tên cột vừa chọn). Ở luồng "Khác", sau khi chọn sheet và cột cho tab `records`, chọn file reference đó áp dụng cho loại dòng nào (chỉ file 1, chỉ file 2, khớp hoàn toàn; mặc định cả 3). Nếu chưa chọn đủ cả 3, tool hỏi có chọn thêm file reference khác cho tab records không; mỗi file chỉ điền note cho loại dòng đã chọn, file chọn trước được ưu tiên, và tất cả ghi vào cùng một cột note (tên cột của file đầu). Ở bước chọn file, bấm Esc (hoặc để trống ô đường dẫn rồi Enter) để trở về câu hỏi có dùng reference không.
7. Chọn đường dẫn lưu file xlsx (mặc định nằm cạnh file 1, tên `comp_<file 1>_vs_<file 2>.xlsx`) → xác nhận → chạy.
Dòng luôn được sắp theo trạng thái, rồi theo cột key.

## Quy tắc so sánh
- Bỏ khoảng trắng đầu/cuối; phân biệt hoa thường; NULL = rỗng = ô trống.
- Số so sánh chính xác dạng thập phân: 1.0 = 1, 12435678654.04001 ≠ 12435678654.03001. Số có số 0 ở đầu (007) coi là chuỗi.
- Ngày quy về yyyy-mm-dd hoặc yyyy-mm-dd hh:mm:ss (nhận: 2026-10-26, 2026/10/26, 26/10/2026, 26-OCT-26, có giờ, AM/PM). 00:00:00 coi như chỉ có ngày.
- Note từ reference được lấy cho mọi dòng của tab records (chỉ file 1, chỉ file 2, khớp hoàn toàn) và mọi dòng của field_mismatch. Dòng header của reference được tự tìm (dòng có nhiều cột key nhất). Nếu sheet reference thiếu một số cột key, tool cảnh báo và khớp note theo các cột còn lại.
- Cột chỉ có ở một bên không được so sánh.
- Tab field_mismatch khớp note theo key và `tên trường` (nếu sheet reference có cột này), nên mỗi trường lệch của cùng một key nhận đúng note của nó.
- File xlsx: đọc giá trị gốc trong ô, nên số lớn giữ đủ chữ số thập phân dù Excel đang hiện làm tròn. Ô có định dạng ngày được đổi về yyyy-mm-dd (có giờ thì thêm hh:mm:ss).

## File kết quả (.xlsx, 3 tab)
- **Tổng hợp**: tên 2 file, tổng số dòng mỗi file, số key chỉ có ở file 1, chỉ có ở file 2, khớp hoàn toàn, khác nhau 1 cột, khác nhau nhiều cột.
- **records**: các key chỉ có ở file 1, chỉ có ở file 2, và các key khớp hoàn toàn. Cột: key, `trạng thái`, các cột chung, cột chỉ có ở file 1 (đuôi `_file1`), cột chỉ có ở file 2 (đuôi `_file2`), cột note nếu có reference.
- **field_mismatch**: mỗi cột khác nhau của một key là một dòng. Cột: key, `tên trường`, `giá trị file 1`, `giá trị file 2`, note nếu có. Các dòng cùng key đứng cạnh nhau, sắp theo tên trường.
- Mọi ô là text (Excel không tự đổi số/ngày), có filter và freeze header + cột key.
- File kết quả có tab `records` và `field_mismatch` có thể dùng lại làm reference cho lần sau.

## Build từ source (máy cá nhân có Go)
Cài Go từ https://go.dev/dl, rồi trong thư mục `comp`:
- Windows: double-click `build.bat`.
- Mac/Linux: chạy `./build.sh`.

Script chạy test rồi tạo `comp_<yyyymmdd_hhmmss>.exe` (Windows 64-bit, ví dụ `comp_20261028_180500.exe`) trong thư mục `exe/`, tự tạo nếu chưa có. Chép file exe sang máy công ty là dùng được.

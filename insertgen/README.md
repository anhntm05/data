# insertgen - Sinh câu lệnh INSERT từ DDL + CSV

Tool CLI (1 file .exe, không cần cài môi trường) để tạo câu lệnh INSERT dump test data cho bảng nguồn.
Nhận DDL `CREATE TABLE` và file CSV (header là tên cột), rồi sinh SQL cho:
- **Oracle**: chạy trong DBeaver (Alt+X để chạy cả script)
- **Athena** trên AWS

## Chạy
Double-click `insertgen.exe` hoặc gõ `insertgen.exe` trong terminal.
Phím bấm giống uatdiff: ↑/↓ chọn, Space tick, Enter xác nhận, / tìm, Ctrl+C thoát.

## Luồng
1. DDL: chọn **bảng đã lưu**, file `.sql/.ddl/.txt`, hoặc dán thẳng vào terminal. Nếu file có nhiều bảng thì chọn 1 bảng.
   Mỗi DDL mới được tự lưu vào thư mục `ddl_library/` cạnh `insertgen.exe` (1 file `.sql` mỗi bảng),
   lần sau chỉ cần chọn từ danh sách. Đưa DDL mới cho cùng bảng thì file cũ được cập nhật.
   Tool hiện danh sách cột + kiểu, cột NOT NULL, cột partition (Athena).
2. Chọn Oracle hay Athena (mặc định theo kiểu DDL).
3. Chọn file CSV. Tool khớp tên cột (không phân biệt hoa thường) và cảnh báo:
   - cột chỉ có trong CSV → bỏ qua
   - cột chỉ có trong DDL → không đưa vào INSERT; nếu là NOT NULL không có DEFAULT thì báo đỏ
4. Bỏ tick cột không muốn insert (vd cột để DB tự sinh).
5. Sửa tên bảng nếu schema ở môi trường test khác DDL.
6. Oracle: chọn mỗi dòng 1 INSERT, hoặc INSERT ALL theo lô 100 dòng.
   Athena: ô trống ở cột chuỗi là NULL hay `''`, và số dòng mỗi câu (tool tự chia để không vượt 256 KB).
7. Biến đổi dữ liệu (tuỳ chọn, thêm được nhiều lần):
   - **Hậu tố tăng dần** cho cột chuỗi: `ABC` → `ABCANHTM01`, `ABCANHTM02`... theo thứ tự dòng trong CSV.
     Chọn được hậu tố, số bắt đầu, số chữ số; tool nhớ hậu tố cho lần sau (`insertgen_settings.json`).
   - **Đặt giá trị mới cho cả cột**, vd đổi `business_date` của mọi dòng sang `03/12/2025`,
     hoặc chỉ đổi các dòng đang là `02/12/2025`. Ngày so khớp theo mọi định dạng (02/12/2025 = 2025-12-02).
   Giá trị sau biến đổi vẫn được kiểm tra kiểu và độ dài như bình thường.
8. Kiểm tra dữ liệu, gom lỗi theo cột: sai kiểu số/ngày/boolean, quá độ dài (Oracle BYTE tính theo byte UTF-8, CHAR tính theo ký tự), NOT NULL bị trống, số quá precision.
   Có lỗi thì chọn: bỏ dòng lỗi / vẫn sinh tất cả / huỷ.
9. Ghi file `<csv>_insert_oracle.sql` hoặc `_athena.sql` cạnh file CSV, có xem trước, copy vào clipboard được.

## Quy tắc chuyển giá trị
| Kiểu cột | Oracle | Athena |
|---|---|---|
| chuỗi | `'O''Brien'` (escape dấu `'`) | giống Oracle |
| CLOB dài > 4000 byte | `TO_CLOB('...') \|\| TO_CLOB('...')` | - |
| số | giữ nguyên chuỗi số, không làm tròn (`12435678654.03001`) | giống Oracle |
| DATE | `TO_DATE('2026-10-26','YYYY-MM-DD')` (có giờ thì `HH24:MI:SS`) | `DATE '2026-10-26'` |
| TIMESTAMP | `TO_TIMESTAMP('2026-10-26 08:30:00.12','YYYY-MM-DD HH24:MI:SS.FF')` | `TIMESTAMP '2026-10-26 08:30:00.12'` |
| boolean | `TRUE/FALSE` | `true/false` (nhận true/false/1/0/y/n) |
| array/map/struct | - | ghi nguyên văn, viết sẵn trong CSV dạng SQL: `ARRAY['a','b']`, `ROW(1,'x')` |

- Ô trống, chỉ có khoảng trắng hoặc chữ `NULL` → `NULL`.
- Ngày nhận nhiều dạng (2026-10-26, 26/10/2026, 26-OCT-26, có giờ, AM/PM). Nếu có ngày dạng 03/04/2026 tool hỏi dd/mm hay mm/dd.
- Giá trị dạng `="00123"` (CSV xuất từ Excel/uatdiff) được bỏ lớp `="..."`.

## Lưu ý bảo mật
Repo này đang để **public**. Không commit DDL, CSV hay file SQL có dữ liệu của công ty.
`ddl_library/` và các file `*_insert_*.sql` đã được `.gitignore` bỏ qua.

## Ví dụ
Thư mục `sample/` có DDL Oracle (kiểu DBeaver export), DDL Athena (SHOW CREATE TABLE) và 1 CSV có cả dòng lỗi để thử.

## Build lại từ source
```
go test ./...
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o insertgen.exe .
```

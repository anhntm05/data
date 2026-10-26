CREATE EXTERNAL TABLE `test_db`.`gd_trans`(
  `trans_code` string COMMENT 'mã giao dịch, chữ + số',
  `statement_num` decimal(16,5),
  `branch_code` varchar(5),
  `cust_name` string,
  `amount` decimal(18,2),
  `seq_no` bigint,
  `is_reversal` boolean,
  `tags` array<string>,
  `extra` struct<a:int,b:string>,
  `txn_date` date,
  `created_at` timestamp)
PARTITIONED BY (
  `ds` string)
ROW FORMAT SERDE
  'org.apache.hadoop.hive.ql.io.parquet.serde.ParquetHiveSerDe'
STORED AS INPUTFORMAT
  'org.apache.hadoop.hive.ql.io.parquet.MapredParquetInputFormat'
OUTPUTFORMAT
  'org.apache.hadoop.hive.ql.io.parquet.MapredParquetOutputFormat'
LOCATION
  's3://bucket/test_db/gd_trans'
TBLPROPERTIES ('parquet.compression'='SNAPPY');

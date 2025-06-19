# Mock Micad

* 模拟micad

## 功能

- 监听 Unix domain socket (`/run/mica/mica-create.socket`)
- 接收并处理控制命令（start、stop、rm、status）
- 打印接收到的所有消息内容
- 返回成功响应

## 编译

```bash
make
```

## 运行

```bash
sudo ./mock_micad
```

注意：需要 root 权限来创建 socket 文件。

## 使用方法

1. 编译并运行 mock_micad
2. 使用 mica.py 或其他 mica 客户端发送命令
3. 观察 mock_micad 的输出，查看接收到的消息内容

## 示例输出

当收到创建消息时：
```
Received Create Message:
CPU: 1
Name: test-client
Path: /path/to/client
Ped: 
PedCfg: 
Debug: false
```

当收到控制命令时：
```
Received control message: start
```

## 清理

```bash
make clean
```

## 注意事项

- 这是一个模拟工具，不会实际执行任何 RTOS 控制操作
- 所有操作都会返回成功响应
- 使用 Ctrl+C 
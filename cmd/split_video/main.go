package main

import (
	"fmt"
	"os/exec"
)

func main() {
	// 硬编码配置
	inputFile := `D:\download\1.mp4`
	splitTime := "01:05:00" // 格式为 HH:MM:SS
	output1 := `D:\download\1_part1.mp4`
	output2 := `D:\download\1_part2.mp4`

	fmt.Printf("正在分割视频: %s\n分割点: %s\n", inputFile, splitTime)

	// 1. 提取前半部分 (0 到 65分钟)
	cmd1 := exec.Command("ffmpeg", "-i", inputFile, "-t", splitTime, "-c", "copy", "-y", output1)
	if err := cmd1.Run(); err != nil {
		fmt.Printf("处理前半部分出错: %v\n", err)
		return
	}
	fmt.Println("完成第一部分:", output1)

	// 2. 提取后半部分 (65分钟 到 结尾)
	// -ss 在 -i 之前可以快速定位，-c copy 保证速度
	cmd2 := exec.Command("ffmpeg", "-ss", splitTime, "-i", inputFile, "-c", "copy", "-y", output2)
	if err := cmd2.Run(); err != nil {
		fmt.Printf("处理后半部分出错: %v\n", err)
		return
	}
	fmt.Println("完成第二部分:", output2)

	fmt.Println("视频分割任务已成功完成。")
}

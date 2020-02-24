package main

func f(A []int,i int){
	var k int
	if (i==1){
		return A[0]
	}else{
		k=f(A,i-1)
		if(k>A[i-1]){
			return A[i-1]
		}else{
			return k
		}
	}
}

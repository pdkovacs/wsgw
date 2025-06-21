import axios from "axios";
import type { UserInfo } from "../../dto/UserInfo";

export const fetchUserInfo = async (): Promise<{userInfo: UserInfo}> => {
	const axiosResponse = await axios({
		method: "GET",
		url: "/user" 
	});
	return {...axiosResponse.data};
};

export const fetchUserList = async (): Promise<string[]> => {
	const axiosResponse = await axios({
		method: "GET",
		url: "/users" 
	});
	return {...axiosResponse.data};
};

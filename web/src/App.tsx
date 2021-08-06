import {FC, useEffect, useMemo, useState} from "react";
import "./App.css";
import LinearProgress from "@material-ui/core/LinearProgress";
import MaterialTable, {Column} from "material-table";
import {createTheme, Snackbar} from "@material-ui/core";
import MuiAlert from "@material-ui/lab/Alert";
import {ThemeProvider} from "@material-ui/styles";
import AppBar from "@material-ui/core/AppBar";
import Typography from "@material-ui/core/Typography";
import Toolbar from "@material-ui/core/Toolbar";
import Box from "@material-ui/core/Box";
import ReconnectingWebSocket from "reconnecting-websocket";

const App: FC = () => {
    const [loading, setLoading] = useState(true);
    const [items, setItems] = useState<Object[]>([]);
    const [lastUpdate, setLastUpdate] = useState("");
    const [updateSnackVisible, setUpdateSnackVisible] = useState(false);

    useEffect(() => {
        let ws = new ReconnectingWebSocket(window.location.origin.replace(/^http/, "ws") + "/ws");
        ws.onopen = () => console.log("Websocket connected");
        ws.onmessage = (msg) => {
            let items = JSON.parse(msg.data);
            setLoading(false);
            setItems(items);
            setLastUpdate(Date.now().toLocaleString())
            setUpdateSnackVisible(true)
        }
    }, [])

    const columns: Column<Object>[] = useMemo(() => {
        if (!items.length) return [];
        let columns: Column<Object>[] = [...new Set(items.flatMap(o=>Object.keys(o)))].map(k => {
                switch (k) {
                    case "url":
                        // Make item URL clickable
                        return {title: k, field: k, render: (rowData: any) => <a href={rowData.url}>{rowData.url}</a>};
                    default:
                        return {title: k, field: k};
                }
            }
        );
        // Add camelcamelcamel price histories
        columns.push({
            title: "history",
            field: "history",
            render: (rowData: any) => <a href={`https://camelcamelcamel.com/product/${rowData.asin}`}
                                         target="_blank"
                                         rel="noreferrer">
                <img alt={"Graphed price history from camelcamelcamel"} style={{mixBlendMode: "multiply"}}
                     src={`https://charts.camelcamelcamel.com/us/${rowData.asin}/amazon-new-used.png?legend=1&fo=1&w=900`}/>
            </a>
        });
        return columns
    }, [items])

    return (
        <ThemeProvider theme={createTheme()}>
            <AppBar position="static">
                <Toolbar>
                    <Typography variant="h6">
                        hdd.cheap
                    </Typography>
                </Toolbar>
            </AppBar>
            <div>
                {
                    loading ?
                        <LinearProgress/> :
                        <Box m={2}>
                            <MaterialTable columns={columns}
                                           data={items}
                                           title={""}
                            />
                            <Snackbar open={updateSnackVisible}
                                      onClose={() => setUpdateSnackVisible(false)}
                                      autoHideDuration={2000}>
                                <MuiAlert severity="info"
                                          elevation={6}
                                          variant="filled">{`Updated ${lastUpdate}`}</MuiAlert>
                            </Snackbar>
                        </Box>
                }
            </div>
        </ThemeProvider>
    )
}

export default App;
